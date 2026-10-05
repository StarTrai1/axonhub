package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

type relayReplayTestDestination struct{ pipeline.DummyMiddleware }

func (m *relayReplayTestDestination) OnOutboundRawRequest(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
	request.URL = "https://relay.invalid/v1/responses"
	return request, nil
}

func TestResponsesRejectedRelayReplayPreservesCiphertextAndHistory(t *testing.T) {
	for _, passThrough := range []bool{false, true} {
		t.Run(map[bool]string{false: "converted", true: "passthrough"}[passThrough], func(t *testing.T) {
			request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse)
			request.Body = []byte(responsesResourceHistoryFixture)
			original := append([]byte(nil), request.Body...)
			executor := &responsesReasoningPipelineExecutor{
				failures: []error{opaqueRelayReplayError()}, events: rejectedReasoningCompactionEvents(),
			}
			ctx := shared.WithSessionScope(t.Context(), "relay-replay-owner")
			state, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, "relay-replay-credential", passThrough, 1,
				func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware { return &relayReplayTestDestination{} })
			require.NoError(t, err)
			drainRejectedReasoningPipeline(t, result)
			require.Len(t, executor.requests, 2)
			before, after := executor.requests[0], executor.requests[1]
			var expected map[string]any
			require.NoError(t, json.Unmarshal(before.Body, &expected))
			for _, item := range expected["input"].([]any) {
				delete(item.(map[string]any), "id")
			}
			encoded, err := json.Marshal(expected)
			require.NoError(t, err)
			require.JSONEq(t, string(encoded), string(after.Body), "only old item IDs may change")
			require.Equal(t, "preserve-native-reasoning", gjson.GetBytes(after.Body, "input.2.encrypted_content").String())
			require.Equal(t, original, request.Body)
			require.Equal(t, before.Headers.Get("Authorization"), after.Headers.Get("Authorization"))
			scope, ok := responsesReasoningScope(ctx, state.CurrentCandidate.Channel, before)
			require.True(t, ok)
			_, remembered := rememberedResponsesReasoningRule(scope, before.Body)
			require.False(t, remembered, "ID detachment must not be learned as permission to drop ciphertext")
		})
	}
}

func TestResponsesRejectedRelayReplaySharesBudgetAndStops(t *testing.T) {
	for _, retries := range []int{0, 1, 4} {
		request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse)
		request.Body = []byte(responsesResourceHistoryFixture)
		executor := &responsesReasoningPipelineExecutor{failures: []error{opaqueRelayReplayError(), opaqueRelayReplayError()}}
		_, _, err := runRejectedReasoningPipeline(t, t.Context(), request, executor, "relay-replay-bounded", true, retries,
			func(*PersistenceState, *PersistentOutboundTransformer) pipeline.Middleware { return &relayReplayTestDestination{} })
		require.Error(t, err)
		want := 2
		if retries == 0 {
			want = 1
		}
		require.Len(t, executor.requests, want)
	}
	request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse)
	request.Body = []byte(responsesResourceHistoryFixture)
	executor := &responsesReasoningPipelineExecutor{failures: []error{opaqueRelayReplayError()}}
	_, _, err := runRejectedReasoningPipeline(t, t.Context(), request, executor, "official-unchanged", true, 3)
	require.Error(t, err)
	require.Len(t, executor.requests, 1, "official endpoints do not use opaque relay error recovery")
}

func TestResponsesRejectedRelayReplayGuards(t *testing.T) {
	body := []byte(responsesResourceHistoryFixture)
	for _, change := range []struct {
		path  string
		value any
	}{
		{"previous_response_id", "resp_external"},
		{"conversation", map[string]string{"id": "conv_external"}},
		{"input.8", map[string]string{"type": "compaction", "id": "cmp_native", "encrypted_content": "checkpoint"}},
		{"input.8", map[string]string{"type": "item_reference", "id": "msg_external"}},
		{"input.8", map[string]string{"type": "future_state"}},
		{"input.5.call_id", "unpaired"},
		{"input.4.encrypted_function_args", "opaque"},
		{"input.5.output", []map[string]string{{"type": "encrypted_content", "encrypted_content": "opaque"}}},
	} {
		changed, err := sjson.SetBytes(body, change.path, change.value)
		require.NoError(t, err)
		_, ok := responsesRelayReplayRule(changed, "", "bad response status code 400", "")
		require.False(t, ok, change.path)
	}
	for _, detail := range []struct{ code, message, param string }{
		{"permission_denied", "bad response status code 400", ""},
		{"", "bad response status code 400", "model"},
		{"", "bad response status code 400: invalid model", ""},
	} {
		_, ok := responsesRelayReplayRule(body, detail.code, detail.message, detail.param)
		require.False(t, ok)
	}
	_, ok := responsesRelayReplayRule(body, "", "bad response status code 400 (request id: synthetic-123)", "")
	require.True(t, ok)
}

func opaqueRelayReplayError() error {
	return &httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"type":"invalid_request_error","message":"bad response status code 400 (request id: synthetic-123)"}}`)}
}
