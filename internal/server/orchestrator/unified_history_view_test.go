package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/httpclient"
)

const (
	unifiedViewOriginal = `{"model":"gpt-6-astra","reasoning":{"effort":"xhigh"},"input":[{"type":"message","id":"u1","role":"user","content":"preserve calibration 17.125"},{"type":"compaction","id":"cmp_prior","encrypted_content":"source-only-state"},{"type":"message","role":"user","content":"continue"}]}`
	unifiedViewAccepted = `{"model":"gpt-6-astra","reasoning":{"effort":"xhigh"},"input":[{"type":"message","role":"user","content":"preserve calibration 17.125"},{"type":"message","role":"assistant","content":"already accepted evidence, never summarize it again"},{"type":"message","role":"user","content":"continue"}]}`
)

func unifiedViewFixture(t *testing.T) (context.Context, *ent.Client, *responsesCompactionRecoveryMiddleware, responsesCompactionRecoveryKey) {
	t.Helper()
	ctx, client, state, adapter := unifiedHistoryFixture(t)
	state.RawRequest = &httpclient.Request{Body: []byte(unifiedViewOriginal)}
	state.RawProviderRequest = &httpclient.Request{Body: []byte(unifiedViewAccepted)}
	m := &responsesCompactionRecoveryMiddleware{adapter: adapter, outbound: &PersistentOutboundTransformer{state: state}}
	key := responsesCompactionRecoveryKey{
		apiKeyID: state.APIKey.ID, projectID: state.APIKey.ProjectID, threadID: "original-thread", cacheKey: "verified-checkpoint",
		scope: responsesReasoningRecoveryScope{
			owner: sha256.Sum256([]byte("owner")), threadID: "wire-thread", windowID: "window-8",
			provider: responsesMetadataCapabilityKey{channelID: state.CurrentCandidate.Channel.ID, model: "gpt-6-astra", url: "https://example.invalid/responses", credential: sha256.Sum256([]byte("credential"))},
		},
	}
	return ctx, client, m, key
}

func growUnifiedViewInput(t *testing.T, body []byte, count int) []byte {
	t.Helper()
	envelope, input, err := decodeResponsesInput(body)
	require.NoError(t, err)
	for i := range count {
		call, err := json.Marshal(map[string]string{"type": "custom_tool_call", "call_id": fmt.Sprintf("new_%d", i), "name": "inspect", "input": "exact new command"})
		require.NoError(t, err)
		result, err := json.Marshal(map[string]string{"type": "custom_tool_call_output", "call_id": fmt.Sprintf("new_%d", i), "output": strings.Repeat("new exact evidence ", 1000)})
		require.NoError(t, err)
		input = append(input, call, result)
	}
	input = append(input, json.RawMessage(`{"type":"message","role":"user","content":"new correction: use 17.250; do not rewrite old evidence"}`))
	require.NoError(t, setResponseEnvelopeInput(envelope, input))
	result, err := json.Marshal(envelope)
	require.NoError(t, err)
	return result
}

func TestUnifiedHistoryAcceptedViewSurvivesRestartAndGrowingResends(t *testing.T) {
	ctx, _, m, key := unifiedViewFixture(t)
	m.prepareUnifiedView(key, m.outbound.state.RawProviderRequest)
	require.NotEmpty(t, m.pendingView)
	m.confirmUnifiedView(ctx)
	// No rejection/source archive/generator is available after restart. An
	// ordinary resend must restore the accepted view without reconstructing it.
	m.adapter = newRemoteCompactionAdapter(nil, nil, m.adapter.systemService)
	for _, count := range []int{1, 20, 40} {
		original := growUnifiedViewInput(t, []byte(unifiedViewOriginal), count)
		m.outbound.state.RawRequest.Body = original
		wire := &httpclient.Request{Body: append([]byte(nil), original...), Headers: make(http.Header)}
		ok, err := m.restoreUnifiedView(ctx, wire, key)
		require.NoError(t, err)
		require.True(t, ok)
		got := gjson.GetBytes(wire.Body, "input").Array()
		prior := gjson.Get(unifiedViewAccepted, "input").Array()
		for i, item := range prior {
			require.Equal(t, item.Raw, got[i].Raw, "the accepted prefix must remain byte-stable")
		}
		require.Len(t, got, len(prior)+count*2+1)
		require.Contains(t, string(wire.Body), "new correction: use 17.250")
		require.NotContains(t, string(wire.Body), "source-only-state")
		require.Contains(t, string(m.outbound.state.RawRequest.Body), "source-only-state", "client/source history remains authoritative")
	}
}

func TestUnifiedHistoryAcceptedViewIsolationAndOverrides(t *testing.T) {
	for _, scenario := range []string{"sibling", "owner", "credential", "model", "thread", "checkpoint", "contract", "wire-override"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, _, m, key := unifiedViewFixture(t)
			m.prepareUnifiedView(key, m.outbound.state.RawProviderRequest)
			m.confirmUnifiedView(ctx)
			m.adapter = newRemoteCompactionAdapter(nil, nil, m.adapter.systemService)
			body := []byte(unifiedViewOriginal)
			switch scenario {
			case "sibling":
				body = []byte(strings.ReplaceAll(string(body), "17.125", "different sibling"))
			case "owner":
				ctx = contexts.WithAPIKey(contexts.CloneRequestContext(ctx), &ent.APIKey{ID: key.apiKeyID + 1, ProjectID: key.projectID})
			case "credential":
				key.scope.provider.credential = sha256.Sum256([]byte("other credential"))
			case "model":
				key.scope.provider.model = "other-model"
			case "thread":
				key.threadID = "other-thread"
			case "checkpoint":
				key.cacheKey = "other-checkpoint"
			case "contract":
				body = []byte(strings.ReplaceAll(string(body), "xhigh", "low"))
			}
			m.outbound.state.RawRequest.Body = body
			wire := &httpclient.Request{Body: append([]byte(nil), body...), Headers: make(http.Header)}
			if scenario == "wire-override" {
				wire.Body = []byte(strings.ReplaceAll(string(body), "17.125", "explicit override"))
			}
			before := append([]byte(nil), wire.Body...)
			ok, err := m.restoreUnifiedView(ctx, wire, key)
			require.NoError(t, err)
			require.False(t, ok)
			require.Equal(t, before, wire.Body)
		})
	}
}

func TestUnifiedHistoryFailedViewIsNotPublished(t *testing.T) {
	ctx, _, m, key := unifiedViewFixture(t)
	m.prepareUnifiedView(key, m.outbound.state.RawProviderRequest)
	alias := m.pendingView
	m.adapted = &key
	_, err := m.OnOutboundRawResponse(ctx, &httpclient.Response{StatusCode: 400, Body: []byte(`{"error":{"message":"failed"}}`)})
	require.NoError(t, err)
	entries, err := m.adapter.systemService.LoadUnifiedHistory(ctx, []string{alias})
	require.NoError(t, err)
	require.Empty(t, entries)
}

func TestUnifiedHistoryRecoveryMiddlewareRestoresPublishedView(t *testing.T) {
	ctx, _, m, _ := unifiedViewFixture(t)
	original, err := sjson.SetBytes([]byte(unifiedViewOriginal), "client_metadata.thread_id", "view-thread")
	require.NoError(t, err)
	newRequest := func(body []byte) *httpclient.Request {
		return &httpclient.Request{
			URL: "https://example.invalid/responses", APIFormat: "openai/responses", Body: body,
			Headers: http.Header{"Authorization": {"Bearer synthetic-view-key"}, "Thread-Id": {"view-thread"}, "X-Codex-Window-Id": {"view-thread:8"}},
		}
	}
	m.outbound.state.RawRequest = newRequest(original)
	key, _, ok := m.recoveryKey(ctx, newRequest(original))
	require.True(t, ok)
	m.prepareUnifiedView(key, m.outbound.state.RawProviderRequest)
	m.adapted = &key
	_, err = m.OnOutboundRawResponse(ctx, &httpclient.Response{StatusCode: 200, Body: []byte(`{"status":"completed","output":[]}`)})
	require.NoError(t, err)
	m.adapter = newRemoteCompactionAdapter(nil, nil, m.adapter.systemService)
	executor := &unifiedConcurrentExecutor{}
	m.executor = executor
	grown := growUnifiedViewInput(t, original, 20)
	m.outbound.state.RawRequest = newRequest(grown)
	got, err := m.OnOutboundRawRequest(ctx, newRequest(grown))
	require.NoError(t, err)
	require.Contains(t, string(got.Body), "already accepted evidence, never summarize it again")
	require.Contains(t, string(got.Body), "new correction: use 17.250")
	require.NotContains(t, string(got.Body), "source-only-state")
	require.Empty(t, executor.summaries)
	require.Contains(t, string(m.outbound.state.RawRequest.Body), "source-only-state")
}

func TestUnifiedHistoryAcceptedViewSurvivesOAuthRefreshButNotAccountChange(t *testing.T) {
	ctx, _, m, key := unifiedViewFixture(t)
	m.outbound.GetCurrentChannel().Credentials = objects.ChannelCredentials{OAuth: &objects.OAuthCredentials{AccessToken: "synthetic-token"}}
	m.outbound.state.RawProviderRequest.Headers = http.Header{"Chatgpt-Account-Id": {"same-account"}}
	m.prepareUnifiedView(key, m.outbound.state.RawProviderRequest)
	m.confirmUnifiedView(ctx)
	m.adapter = newRemoteCompactionAdapter(nil, nil, m.adapter.systemService)
	key.scope.provider.credential = sha256.Sum256([]byte("refreshed-token"))
	for _, account := range []string{"same-account", "different-account"} {
		wire := &httpclient.Request{Body: []byte(unifiedViewOriginal), Headers: http.Header{"Chatgpt-Account-Id": {account}}}
		ok, err := m.restoreUnifiedView(ctx, wire, key)
		require.NoError(t, err)
		require.Equal(t, account == "same-account", ok)
	}
}

func TestUnifiedHistoryImportsVerifiedCompletedViewOnUpgrade(t *testing.T) {
	for _, scenario := range []string{"same", "other-account", "other-window", "failed", "forged-summary", "opaque-checkpoint"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, client, m, key := unifiedViewFixture(t)
			channel := m.outbound.GetCurrentChannel()
			channel.Credentials = objects.ChannelCredentials{OAuth: &objects.OAuthCredentials{AccessToken: "synthetic-oauth"}}
			channel.UpdatedAt = time.Now().Add(time.Minute) // refreshed after the historical execution
			summaryKey := "summary:" + strings.Repeat("a", 64)
			text := "verified old summary 17.125"
			sealed, err := m.adapter.sealHistoryRecord(ctx, summaryKey, []byte(text))
			require.NoError(t, err)
			require.NoError(t, m.adapter.systemService.SaveUnifiedHistory(ctx, map[string]string{summaryKey: sealed}))
			note := "Archived history summary (source " + strings.Repeat("a", 64) + "). This is a derived summary; the original user instructions remain authoritative.\n" + text
			if scenario == "forged-summary" {
				note += "not in authenticated summary"
			}
			view, err := sjson.SetBytes([]byte(unifiedViewAccepted), "input.1.content", []map[string]string{{"type": "output_text", "text": note}})
			require.NoError(t, err)
			if scenario == "opaque-checkpoint" {
				view, err = sjson.SetRawBytes(view, "input.-1", []byte(`{"type":"compaction","id":"cmp_foreign","encrypted_content":"unreadable"}`))
				require.NoError(t, err)
			}
			parent, err := client.Request.Create().SetAPIKeyID(key.apiKeyID).SetProjectID(key.projectID).SetModelID("gpt-6-astra").SetFormat("openai/responses-ws").SetStatus(request.StatusCompleted).SetRequestBody([]byte(unifiedViewOriginal)).Save(ctx)
			require.NoError(t, err)
			headers := http.Header{"Chatgpt-Account-Id": {"account-one"}, "Thread-Id": {"wire-thread"}, "X-Codex-Window-Id": {"window-8"}}
			if scenario == "other-account" {
				headers.Set("Chatgpt-Account-Id", "other-account")
			}
			if scenario == "other-window" {
				headers.Set("X-Codex-Window-Id", "window-9")
			}
			rawHeaders, err := json.Marshal(headers)
			require.NoError(t, err)
			status := requestexecution.StatusCompleted
			if scenario == "failed" {
				status = requestexecution.StatusFailed
			}
			_, err = client.RequestExecution.Create().SetRequestID(parent.ID).SetProjectID(key.projectID).SetChannelID(channel.ID).SetModelID("gpt-6-astra").SetFormat("openai/responses-ws").SetStatus(status).SetRequestURL(key.scope.provider.url).SetRequestHeaders(rawHeaders).SetRequestBody(view).Save(ctx)
			require.NoError(t, err)
			original := growUnifiedViewInput(t, []byte(unifiedViewOriginal), 2)
			m.outbound.state.RawRequest.Body = original
			wire := &httpclient.Request{URL: key.scope.provider.url, Body: append([]byte(nil), original...), Headers: http.Header{"Chatgpt-Account-Id": {"account-one"}}}
			ok, err := m.restoreUnifiedView(ctx, wire, key)
			require.NoError(t, err)
			require.Equal(t, scenario == "same" || scenario == "other-window", ok)
			if ok {
				require.Contains(t, string(wire.Body), text)
				require.Contains(t, string(wire.Body), "new correction: use 17.250")
				m.adapter = newRemoteCompactionAdapter(nil, nil, m.adapter.systemService)
				wire.Body = original
				ok, err = m.restoreUnifiedView(ctx, wire, key)
				require.NoError(t, err)
				require.True(t, ok, "import must survive log removal and restart")
			}
		})
	}
}
