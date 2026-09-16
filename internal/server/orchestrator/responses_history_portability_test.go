package orchestrator

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	entrequest "github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
)

const (
	portableHistoryURL        = "https://portable-history.example/v1/responses"
	portableHistoryCredential = "synthetic-portable-key-1234"
	portableHistoryBefore     = `{"model":"gpt-6-astra","stream":true,"store":false,"input":[
{"type":"message","id":"msg_old","role":"user","content":"complete task"},
{"type":"reasoning","id":"rs_old","encrypted_content":"old-ciphertext","summary":[{"type":"summary_text","text":"visible summary"}]},
{"type":"custom_tool_call","id":"ctc_old","call_id":"call_one","name":"exec","input":"exact command"},
{"type":"custom_tool_call_output","id":"ctco_old","call_id":"call_one","output":"exact result"}]}`
	portableHistoryAfter = `{"model":"gpt-6-astra","stream":true,"store":false,"input":[
{"type":"message","role":"user","content":"complete task"},
{"type":"message","role":"assistant","content":[{"type":"output_text","text":"visible summary"}]},
{"type":"custom_tool_call","call_id":"call_one","name":"exec","input":"exact command"},
{"type":"custom_tool_call_output","call_id":"call_one","output":"exact result"}]}`
)

func seedPortableHistoryPair(t *testing.T, ctx context.Context, client *ent.Client, apiKey *ent.APIKey, before, after, message string) {
	t.Helper()
	parent, err := client.Request.Create().SetProjectID(apiKey.ProjectID).SetAPIKeyID(apiKey.ID).
		SetModelID("gpt-6-astra").SetFormat(string(llm.APIFormatOpenAIResponse)).SetStatus(entrequest.StatusCompleted).
		SetRequestBody([]byte(before)).Save(ctx)
	require.NoError(t, err)
	_, err = client.RequestExecution.Create().SetRequestID(parent.ID).SetProjectID(apiKey.ProjectID).SetChannelID(98001).
		SetModelID("gpt-6-astra").SetFormat(string(llm.APIFormatOpenAIResponse)).SetStatus(requestexecution.StatusFailed).
		SetRequestURL(portableHistoryURL).SetChannelAPIKeySuffix("1234").SetResponseStatusCode(400).
		SetErrorMessage(message).SetRequestBody([]byte(before)).Save(ctx)
	require.NoError(t, err)
	_, err = client.RequestExecution.Create().SetRequestID(parent.ID).SetProjectID(apiKey.ProjectID).SetChannelID(98001).
		SetModelID("gpt-6-astra").SetFormat(string(llm.APIFormatOpenAIResponse)).SetStatus(requestexecution.StatusCompleted).
		SetRequestURL(portableHistoryURL).SetChannelAPIKeySuffix("1234").SetRequestBody([]byte(after)).Save(ctx)
	require.NoError(t, err)
}

func portableHistoryOutbound(apiKey *ent.APIKey, service *biz.RequestService) (*PersistentOutboundTransformer, *httpclient.Request) {
	channel := &biz.Channel{Channel: &ent.Channel{
		ID: 98001, Type: entchannel.TypeCodex, UpdatedAt: time.Now().Add(-time.Hour),
		Credentials: objects.ChannelCredentials{APIKey: portableHistoryCredential},
	}}
	outbound := &PersistentOutboundTransformer{state: &PersistenceState{
		APIKey: apiKey, RequestService: service, CurrentCandidate: &ChannelModelsCandidate{Channel: channel},
	}}
	request := &httpclient.Request{
		URL: portableHistoryURL, APIFormat: string(llm.APIFormatOpenAIResponse), Body: []byte(portableHistoryBefore),
		Headers: http.Header{"Authorization": {"Bearer " + portableHistoryCredential}, "X-Codex-Turn-State": {"old-sticky-token"}},
	}
	return outbound, request
}

func TestResponsesRejectedHistoryPortabilityFirstAttemptAfterRestart(t *testing.T) {
	for _, passThrough := range []bool{true, false} {
		t.Run(map[bool]string{true: "native", false: "converted"}[passThrough], func(t *testing.T) {
			ctx, client, apiKey, adapter := rejectedCompactionStorage(t)
			for range 2 {
				seedPortableHistoryPair(t, ctx, client, apiKey, portableHistoryBefore, portableHistoryAfter, responsesResourceMismatchMessage)
			}
			// A new service represents a restarted process, with no rejection cache.
			service := createTestRequestService(t, client)
			require.NotSame(t, adapter.requestService, service)
			ctx = contexts.WithChannelAPIKey(ctx, portableHistoryCredential)
			req := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse)
			req.Body = []byte(strings.ReplaceAll(strings.ReplaceAll(portableHistoryBefore, "_old", "_new"), "old-ciphertext", "new-ciphertext"))
			executor := &responsesReasoningPipelineExecutor{events: rejectedReasoningCompactionEvents()}
			state, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, portableHistoryCredential, passThrough, 0,
				func(state *PersistenceState, _ *PersistentOutboundTransformer) pipeline.Middleware {
					state.APIKey, state.RequestService = apiKey, service
					channel := state.ChannelModelsCandidates[0].Channel
					channel.UpdatedAt = time.Now().Add(-time.Hour)
					channel.Credentials = objects.ChannelCredentials{APIKey: portableHistoryCredential}
					return pipeline.OnRawRequest("final-target", func(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
						request.URL = portableHistoryURL
						return request, nil
					})
				},
				func(_ *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
					return applyResponsesHistoryPortability(outbound)
				})
			require.NoError(t, err)
			drainRejectedReasoningPipeline(t, result)
			require.Len(t, executor.requests, 1, "must succeed with a zero retry budget and new IDs/ciphertext")
			require.JSONEq(t, gjson.Get(portableHistoryAfter, "input").Raw, gjson.GetBytes(executor.requests[0].Body, "input").Raw)
			require.Equal(t, "gpt-6-astra", gjson.GetBytes(executor.requests[0].Body, "model").String())
			require.Zero(t, state.responsesRejectedStatusRetryChannel)
			require.Contains(t, string(req.Body), "new-ciphertext", "client history stays intact")
		})
	}
}

func TestResponsesRejectedHistoryPortabilityIsolation(t *testing.T) {
	for _, name := range []string{"same", "channel", "model", "endpoint", "caller", "project", "updated-channel", "credential", "ambiguous-suffix", "auth-override"} {
		t.Run(name, func(t *testing.T) {
			ctx, client, apiKey, adapter := rejectedCompactionStorage(t)
			for range 2 {
				seedPortableHistoryPair(t, ctx, client, apiKey, portableHistoryBefore, portableHistoryAfter, responsesResourceMismatchMessage)
			}
			ctx = contexts.WithChannelAPIKey(ctx, portableHistoryCredential)
			outbound, request := portableHistoryOutbound(apiKey, adapter.requestService)
			channel := outbound.GetCurrentChannel()
			switch name {
			case "channel":
				channel.ID++
			case "model":
				request.Body = []byte(strings.ReplaceAll(portableHistoryBefore, "gpt-6-astra", "other-model"))
			case "endpoint":
				request.URL += "/other"
			case "caller":
				outbound.state.APIKey = &ent.APIKey{ID: apiKey.ID + 1, ProjectID: apiKey.ProjectID}
			case "project":
				outbound.state.APIKey = &ent.APIKey{ID: apiKey.ID, ProjectID: apiKey.ProjectID + 1}
			case "updated-channel":
				channel.UpdatedAt = time.Now().Add(time.Second)
			case "credential":
				ctx = contexts.WithChannelAPIKey(ctx, "changed-key-5678")
				channel.Credentials.APIKey = "changed-key-5678"
				request.Headers.Set("Authorization", "Bearer changed-key-5678")
			case "ambiguous-suffix":
				channel.Credentials.APIKeys = []string{"other-key-1234"}
			case "auth-override":
				request.Auth = &httpclient.AuthConfig{Type: httpclient.AuthTypeBearer, APIKey: "override-key-1234"}
			}
			original := string(request.Body)
			request.JSONBody = append([]byte(nil), request.Body...)
			result, err := applyResponsesHistoryPortability(outbound).OnOutboundRawRequest(ctx, request)
			require.NoError(t, err)
			if name == "same" {
				require.JSONEq(t, portableHistoryAfter, string(result.Body))
				require.Equal(t, result.Body, result.JSONBody)
				require.Empty(t, result.Headers.Get(codexTurnStateHeader))
			} else {
				require.Equal(t, original, string(result.Body))
			}
			require.Zero(t, outbound.state.responsesRejectedStatusRetryChannel)
		})
	}
}

func TestResponsesRejectedHistoryPortabilityRequiresConfirmedPairs(t *testing.T) {
	for _, name := range []string{"single", "different-content", "different-options", "unrelated-error", "wrong-encrypted-item"} {
		t.Run(name, func(t *testing.T) {
			ctx, client, apiKey, adapter := rejectedCompactionStorage(t)
			after, message, count := portableHistoryAfter, responsesResourceMismatchMessage, 2
			switch name {
			case "single":
				count = 1
			case "different-content":
				after = strings.ReplaceAll(after, "exact result", "missing result")
			case "different-options":
				after = strings.ReplaceAll(after, `"store":false`, `"store":true`)
			case "unrelated-error":
				message = "upstream bad request"
			case "wrong-encrypted-item":
				message = "The encrypted content for item rs_missing could not be verified. Reason: Encrypted content could not be decrypted or parsed."
			}
			for range count {
				seedPortableHistoryPair(t, ctx, client, apiKey, portableHistoryBefore, after, message)
			}
			ctx = contexts.WithChannelAPIKey(ctx, portableHistoryCredential)
			outbound, request := portableHistoryOutbound(apiKey, adapter.requestService)
			result, err := applyResponsesHistoryPortability(outbound).OnOutboundRawRequest(ctx, request)
			require.NoError(t, err)
			require.Equal(t, portableHistoryBefore, string(result.Body))
		})
	}
}

func TestResponsesRejectedHistoryPortabilityPreservesOpaqueHistory(t *testing.T) {
	for _, scenario := range []struct {
		path  string
		value any
	}{
		{"input.-1", map[string]string{"type": "compaction", "id": "cmp_native", "encrypted_content": "opaque-checkpoint"}},
		{"previous_response_id", "resp_stored"},
		{"input.3.call_id", "missing-call"},
		{"input.3.output", []map[string]string{{"type": "encrypted_content", "encrypted_content": "opaque-tool-result"}}},
	} {
		t.Run(scenario.path, func(t *testing.T) {
			body, err := sjson.SetBytes([]byte(portableHistoryBefore), scenario.path, scenario.value)
			require.NoError(t, err)
			result, changed, err := portableResponsesHistory(body, responsesHistoryPolicy{detachIDs: true, dropReasoning: true})
			require.NoError(t, err)
			require.False(t, changed)
			require.Equal(t, body, result)
		})
	}
	result, changed, err := portableResponsesHistory([]byte(portableHistoryBefore), responsesHistoryPolicy{detachIDs: true, dropReasoning: true})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "call_one", gjson.GetBytes(result, "input.2.call_id").String())
	require.Equal(t, "call_one", gjson.GetBytes(result, "input.3.call_id").String())
	require.JSONEq(t, portableHistoryAfter, string(result))
}

func TestResponsesRejectedHistoryPortabilityLearnsFieldsIndependently(t *testing.T) {
	ctx, client, apiKey, adapter := rejectedCompactionStorage(t)
	idsOnly := `{"model":"gpt-6-astra","stream":true,"store":false,"input":[
{"type":"message","role":"user","content":"complete task"},
{"type":"reasoning","id":"rs_old","encrypted_content":"old-ciphertext","summary":[{"type":"summary_text","text":"visible summary"}]},
{"type":"custom_tool_call","call_id":"call_one","name":"exec","input":"exact command"},
{"type":"custom_tool_call_output","call_id":"call_one","output":"exact result"}]}`
	for range 2 {
		seedPortableHistoryPair(t, ctx, client, apiKey, portableHistoryBefore, idsOnly, responsesResourceMismatchMessage)
	}
	ctx = contexts.WithChannelAPIKey(ctx, portableHistoryCredential)
	outbound, request := portableHistoryOutbound(apiKey, adapter.requestService)
	result, err := applyResponsesHistoryPortability(outbound).OnOutboundRawRequest(ctx, request)
	require.NoError(t, err)
	require.JSONEq(t, idsOnly, string(result.Body), "ID-only evidence must retain native reasoning")
}

func TestResponsesRejectedHistoryPortabilityRejectsNumericOptionChanges(t *testing.T) {
	before, err := sjson.SetRawBytes([]byte(portableHistoryBefore), "custom_option", []byte("9007199254740992"))
	require.NoError(t, err)
	after, err := sjson.SetRawBytes([]byte(portableHistoryAfter), "custom_option", []byte("9007199254740993"))
	require.NoError(t, err)
	policy := confirmedResponsesHistoryCorrection(before, after, responsesResourceMismatchMessage)
	require.False(t, policy.detachIDs)
	require.False(t, policy.dropReasoning)
}
