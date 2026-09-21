package orchestrator

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

const preservedReasoningCheckpoint = `{"type":"compaction","id":"cmp_preserved","encrypted_content":"opaque-native-checkpoint"}`

func TestResponsesRejectedReasoningPreservesNativeCheckpointPipeline(t *testing.T) {
	for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseCompact} {
		for _, passThrough := range []bool{true, false} {
			t.Run(string(format)+map[bool]string{true: "/raw", false: "/converted"}[passThrough], func(t *testing.T) {
				ctx := shared.WithSessionScope(t.Context(), "checkpoint-owner")
				request := rejectedReasoningPipelineRequest(t, format)
				items := []json.RawMessage{json.RawMessage(preservedReasoningCheckpoint)}
				for _, item := range gjson.GetBytes(request.Body, "input").Array() {
					items = append(items, json.RawMessage(item.Raw))
				}
				var err error
				request.Body, err = sjson.SetBytes(request.Body, "input", items)
				require.NoError(t, err)
				original := append([]byte(nil), request.Body...)
				executor := &responsesReasoningPipelineExecutor{
					failures: []error{&httpclient.Error{StatusCode: 400, Body: []byte(`{"error":{"code":"invalid_encrypted_content","message":"The encrypted content for item rs_visible could not be verified. Reason: Encrypted content could not be decrypted or parsed."}}`)}},
					events:   rejectedReasoningCompactionEvents(),
					response: &httpclient.Response{StatusCode: http.StatusOK, Body: []byte(`{"id":"cmp_next","object":"response.compaction","output":[{"type":"compaction","id":"cmp_new","encrypted_content":"new-checkpoint"}]}`)},
				}
				nativePolicy := func(state *PersistenceState, _ *PersistentOutboundTransformer) pipeline.Middleware {
					state.ChannelModelsCandidates[0].Channel.Policies.RemoteCompaction = objects.RemoteCompactionPolicyNative
					return pipeline.DummyMiddleware{}
				}
				state, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, "checkpoint-target", passThrough, 1, nativePolicy)
				require.NoError(t, err)
				if result.Stream {
					drainRejectedReasoningPipeline(t, result)
				}
				require.Len(t, executor.requests, 2)
				retry := executor.requests[1].Body
				require.Equal(t, preservedReasoningCheckpoint, gjson.GetBytes(retry, "input.0").Raw)
				require.Empty(t, encryptedResponsesReasoningHashes(retry))
				require.Equal(t, original, request.Body)
				for _, item := range gjson.GetBytes(original, "input").Array() {
					if item.Get("type").String() != "reasoning" {
						require.Contains(t, string(retry), item.Raw, "preserve every message, tool call/output, checkpoint and control")
					}
				}
				require.Contains(t, string(retry), "visible summary")
				require.Contains(t, string(retry), "visible rationale")
				scope, ok := responsesReasoningScope(ctx, state.CurrentCandidate.Channel, executor.requests[0])
				require.True(t, ok)
				t.Cleanup(func() { responsesReasoningRecoveries.Remove(scope) })
				require.Eventually(t, func() bool {
					_, found := rememberedResponsesReasoningRule(scope, original)
					return found
				}, time.Second, time.Millisecond)
				request.Body, err = sjson.SetRawBytes(original, "input.-1", []byte(`{"type":"reasoning","id":"rs_target_new","encrypted_content":"new-target-reasoning"}`))
				require.NoError(t, err)
				next := &responsesReasoningPipelineExecutor{events: executor.events, response: executor.response}
				_, result, err = runRejectedReasoningPipeline(t, ctx, request, next, "checkpoint-target", passThrough, 0, nativePolicy)
				require.NoError(t, err)
				if result.Stream {
					drainRejectedReasoningPipeline(t, result)
				}
				require.Len(t, next.requests, 1)
				require.Equal(t, preservedReasoningCheckpoint, gjson.GetBytes(next.requests[0].Body, "input.0").Raw)
				require.Len(t, encryptedResponsesReasoningHashes(next.requests[0].Body), 1)
				require.Equal(t, "new-target-reasoning", gjson.GetBytes(next.requests[0].Body, `input.#(id=="rs_target_new").encrypted_content`).String())
			})
		}
	}
}

func TestResponsesRejectedReasoningNativeCheckpointGuards(t *testing.T) {
	body, err := sjson.SetRawBytes([]byte(responsesRejectedReasoningFixture), "input.-1", []byte(preservedReasoningCheckpoint))
	require.NoError(t, err)
	_, ok := responsesRejectedReasoningRule(body, "input[1].encrypted_content")
	require.True(t, ok)
	_, ok = responsesRejectedReasoningRule(body, "")
	require.False(t, ok, "an unindexed encryption error may refer to the checkpoint")
	_, ok = responsesRejectedReasoningRule(body, "input[9].encrypted_content")
	require.False(t, ok, "checkpoint errors must use checkpoint recovery")
	for _, change := range []struct {
		path string
		value any
	}{
		{"previous_response_id", "resp_external"},
		{"conversation", "conv_external"},
		{"input.9.id", "cmp_axonhub_unknown"},
		{"input.9.id", ""},
		{"input.9.encrypted_content", ""},
		{"input.10", map[string]any{"type": "compaction", "id": "cmp_second", "encrypted_content": "other"}},
		{"input.3.call_id", "orphan"},
		{"input.2.encrypted_function_args", "opaque"},
		{"input.3.output", []any{map[string]any{"type": "encrypted_content", "encrypted_content": "opaque"}}},
		{"input.10", map[string]any{"type": "item_reference", "id": "external"}},
	} {
		changed, err := sjson.SetBytes(body, change.path, change.value)
		require.NoError(t, err)
		_, accepted := responsesRejectedReasoningRule(changed, "input[1].encrypted_content")
		require.False(t, accepted, change.path)
	}
}
