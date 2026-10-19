package orchestrator

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestUnifiedHistoryResumeNormalizesOnlyCompletedTransportMetadata(t *testing.T) {
	for _, scenario := range []string{"completed", "call-id", "arguments", "unfinished", "tool-schema", "ciphertext"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, _, m, key := unifiedViewFixture(t)
			tool := gjson.Get(portableAgentToolFixture, "input.0").Raw
			original := []byte(`{"model":"gpt-6-astra","reasoning":{"effort":"xhigh"},"input":[` + tool + `,{"type":"compaction","id":"cmp_prior","encrypted_content":"source-only-state"},{"type":"custom_tool_call","id":"ctc_one","call_id":"call_one","name":"inspect","input":"calibration 17.125","status":"completed"},{"type":"custom_tool_call_output","call_id":"call_one","output":"exact evidence"},{"type":"reasoning","id":"rs_one","encrypted_content":"retained-private-state","status":"completed"}]}`)
			m.outbound.state.RawRequest.Body = original
			// Publish the old format, as a running installation did before this fix.
			legacy, err := unifiedViewKeysVersion(key, original, true)
			require.NoError(t, err)
			require.NoError(t, m.adapter.saveHistorySnapshot(ctx, legacy[len(legacy)-1], []byte(unifiedViewAccepted), nil))
			grown := growUnifiedViewInput(t, original, 1)
			m.outbound.state.RawRequest.Body = grown
			wire := append([]byte(nil), grown...)
			wire, err = sjson.DeleteBytes(wire, "input.2.status")
			require.NoError(t, err)
			wire, err = sjson.DeleteBytes(wire, "input.4.status")
			require.NoError(t, err)
			wire, err = sjson.SetBytes(wire, "input.0.tools.0.tools.0.parameters.properties.message.encrypted", false)
			require.NoError(t, err)
			paths := map[string][2]string{
				"call-id":     {"input.2.call_id", "different-call"},
				"arguments":   {"input.2.input", "different command"},
				"unfinished":  {"input.2.status", "in_progress"},
				"tool-schema": {"input.0.tools.0.tools.0.parameters.properties.message.type", "number"},
				"ciphertext":  {"input.4.encrypted_content", "different private state"},
			}
			if change, ok := paths[scenario]; ok {
				wire, err = sjson.SetBytes(wire, change[0], change[1])
				require.NoError(t, err)
			}
			outgoing := &httpclient.Request{Body: wire, Headers: make(http.Header)}
			restored, err := m.restoreUnifiedView(ctx, outgoing, key)
			require.NoError(t, err)
			require.Equal(t, scenario == "completed", restored)
			if !restored {
				require.Equal(t, wire, outgoing.Body)
				return
			}
			require.Contains(t, string(outgoing.Body), "already accepted evidence")
			require.Contains(t, string(outgoing.Body), "17.250")
			require.NotContains(t, string(outgoing.Body), "source-only-state")
			// A changed connection window reuses the migrated v2 alias without logs.
			key.scope.windowID = "new-window"
			m.adapter = newRemoteCompactionAdapter(nil, nil, m.adapter.systemService)
			outgoing.Body = wire
			restored, err = m.restoreUnifiedView(ctx, outgoing, key)
			require.NoError(t, err)
			require.True(t, restored)
		})
	}
}

func TestUnifiedHistoryResumeImportsLegacyWindowWithDeltaLog(t *testing.T) {
	ctx, client, m, key := unifiedViewFixture(t)
	channel := m.outbound.GetCurrentChannel()
	channel.Credentials = objects.ChannelCredentials{OAuth: &objects.OAuthCredentials{AccessToken: "synthetic-token"}}
	destination := &httpclient.Request{URL: key.scope.provider.url, Headers: http.Header{"Chatgpt-Account-Id": {"same-account"}}}
	scoped := m.unifiedViewScope(key, destination)
	legacy, err := unifiedViewKeysVersion(scoped, []byte(unifiedViewOriginal), true)
	require.NoError(t, err)
	require.NoError(t, m.adapter.saveHistorySnapshot(ctx, legacy[len(legacy)-1], []byte(unifiedViewAccepted), nil))
	parent, err := client.Request.Create().SetAPIKeyID(key.apiKeyID).SetProjectID(key.projectID).SetModelID("gpt-6-astra").SetFormat("openai/responses-ws").SetStatus(request.StatusCompleted).SetRequestBody([]byte(`{"previous_response_id":"old-connection","input":[]}`)).Save(ctx)
	require.NoError(t, err)
	headers, err := json.Marshal(http.Header{"Chatgpt-Account-Id": {"same-account"}, "Thread-Id": {key.scope.threadID}, "X-Codex-Window-Id": {key.scope.windowID}})
	require.NoError(t, err)
	_, err = client.RequestExecution.Create().SetRequestID(parent.ID).SetProjectID(key.projectID).SetChannelID(channel.ID).SetModelID("gpt-6-astra").SetFormat("openai/responses-ws").SetStatus(requestexecution.StatusCompleted).SetRequestURL(destination.URL).SetRequestHeaders(headers).SetRequestBody([]byte(unifiedViewAccepted)).Save(ctx)
	require.NoError(t, err)
	key.scope.windowID = "window-after-restart"
	grown := growUnifiedViewInput(t, []byte(unifiedViewOriginal), 1)
	m.outbound.state.RawRequest.Body = grown
	destination.Body = grown
	restored, err := m.restoreUnifiedView(ctx, destination, key)
	require.NoError(t, err)
	require.True(t, restored, "the old accepted alias authenticates the complete current prefix despite a delta request log")
	require.Contains(t, string(destination.Body), "already accepted evidence")
}

func TestUnifiedHistoryMoreThan32SegmentsReusesCachedOriginals(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	state.CurrentCandidate.ContextTokens = 0
	executor := &unifiedConcurrentExecutor{}
	_, ok, err := adapter.compileUnifiedContext(ctx, unifiedLongToolHistory(t, 64), state, executor)
	require.NoError(t, err)
	require.True(t, ok)
	prior := len(executor.summaries)
	require.LessOrEqual(t, prior, unifiedMaxNewSummaries)
	compiled, ok, err := adapter.compileUnifiedContext(ctx, unifiedLongToolHistory(t, 70), state, executor)
	require.NoError(t, err)
	require.True(t, ok)
	require.Greater(t, len(executor.summaries), 32)
	require.LessOrEqual(t, len(executor.summaries)-prior, 4)
	require.Contains(t, string(compiled), "17.125")
	require.Contains(t, string(compiled), "17.250")
	for _, summary := range executor.summaries {
		require.NotContains(t, string(summary), "Archived history summary")
	}
}

func TestUnifiedHistoryPreparationLimitsDoNotStartWholeHistorySummary(t *testing.T) {
	for _, scenario := range []string{"too-many-new", "indivisible-tool"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, _, state, adapter := unifiedHistoryFixture(t)
			state.CurrentCandidate.ContextTokens = 0
			executor := &unifiedConcurrentExecutor{}
			body := unifiedLongToolHistory(t, 70)
			if scenario == "indivisible-tool" {
				var err error
				body, err = sjson.SetBytes(body, "input.2.output", strings.Repeat("oversized exact evidence ", 10000))
				require.NoError(t, err)
			}
			_, ok, err := adapter.compileUnifiedContext(ctx, body, state, executor)
			require.False(t, ok)
			var responseError *llm.ResponseError
			require.ErrorAs(t, err, &responseError)
			require.Equal(t, "unified_history_budget_exceeded", responseError.Detail.Code)
			require.Empty(t, executor.summaries, "budget failure cannot launch a larger legacy summary")
		})
	}
}
