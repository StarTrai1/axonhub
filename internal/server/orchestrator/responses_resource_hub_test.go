package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
)

// AgentRouter adds this wrapper to both native and redacted resource errors.
// Keep synthetic trace identifiers and payloads; production histories stay private.
const responsesResourceHubMessage = "OpenAI Responses bad request: " + responsesResourceMismatchMessage + " [hub-] [trace_id=synthetic-trace]"

func TestAzureRecoveryHubWrapperRetriesOnce(t *testing.T) {
	for _, resource := range []string{"Azure", "***"} {
		for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseWebSocket} {
			for _, retries := range []int{0, 4} {
				for _, repeat := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/budget%d/repeat%t", resource, format, retries, repeat), func(t *testing.T) {
						message := strings.ReplaceAll(responsesResourceHubMessage, "Azure", resource)
						var failure error = &llm.ResponseError{StatusCode: http.StatusBadRequest, Detail: llm.ErrorDetail{Type: "invalid_request_error", Message: message}}
						if format == llm.APIFormatOpenAIResponse {
							body, err := sjson.SetBytes([]byte(`{"error":{"type":"invalid_request_error"}}`), "error.message", message)
							require.NoError(t, err)
							failure = &httpclient.Error{StatusCode: http.StatusBadRequest, Body: body}
						}
						request := rejectedReasoningPipelineRequest(t, format)
						request.Body = []byte(responsesResourceHistoryFixture)
						original := append([]byte(nil), request.Body...)
						executor := &responsesReasoningPipelineExecutor{failures: []error{failure}, events: rejectedReasoningCompactionEvents()}
						if repeat {
							executor.failures = append(executor.failures, failure)
						}
						_, result, err := runRejectedReasoningPipeline(t, t.Context(), request, executor, "synthetic", true, retries,
							hubRecoveryRelayTarget,
							func(_ *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
								return applyAzureResponsesHistoryCompatibility(outbound)
							})
						require.Equal(t, original, request.Body)
						require.JSONEq(t, gjson.GetBytes(original, "input").Raw, gjson.GetBytes(executor.requests[0].Body, "input").Raw)
						if retries == 0 {
							require.Len(t, executor.requests, 1)
							require.Error(t, err)
							require.Nil(t, result)
							return
						}
						require.Len(t, executor.requests, 2, "the exact rejection unlocks one recovery, including when recovery is rejected")
						replayed := executor.requests[1]
						require.True(t, replayed.ResponsesStateless)
						require.Empty(t, replayed.Headers.Get(codexTurnStateHeader))
						require.Equal(t, executor.requests[0].Headers.Get("Authorization"), replayed.Headers.Get("Authorization"))
						require.Equal(t, "visible reasoning summary", gjson.GetBytes(replayed.Body, "input.2.content.0.text").String())
						for _, item := range gjson.GetBytes(replayed.Body, "input").Array() {
							require.False(t, item.Get("id").Exists())
							require.False(t, item.Get("encrypted_content").Exists())
						}
						for _, path := range []string{"input.1.content", "input.3.content", "input.4.call_id", "input.4.arguments", "input.5.call_id", "input.5.output", "input.6.call_id", "input.6.input", "input.7.call_id", "input.7.output", "prompt_cache_key", "reasoning"} {
							require.Equal(t, gjson.GetBytes(original, path).Raw, gjson.GetBytes(replayed.Body, path).Raw, path)
						}
						if repeat {
							require.Error(t, err)
							require.Nil(t, result)
						} else {
							require.NoError(t, err)
							drainRejectedReasoningPipeline(t, result)
						}
					})
				}
			}
		}
	}
}

func hubRecoveryRelayTarget(state *PersistenceState, _ *PersistentOutboundTransformer) pipeline.Middleware {
	state.ChannelModelsCandidates[0].Channel.Credentials = objects.ChannelCredentials{APIKey: "synthetic"}
	return pipeline.OnRawRequest("relay-target", func(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		request.URL = "https://relay.example/v1/responses"
		return request, nil
	})
}

func TestAzureRecoveryHubWrapperRetainsCheckpointHistory(t *testing.T) {
	for _, resource := range []string{"Azure", "***"} {
		t.Run(resource, func(t *testing.T) {
			ctx, client, apiKey, adapter := rejectedCompactionStorage(t)
			seedRejectedCompactionSource(t, ctx, client, apiKey, "completed", "native-checkpoint-ciphertext")
			executor := &rejectedCompactionBridgeExecutor{rejection: &llm.ResponseError{
				StatusCode: http.StatusBadRequest,
				Detail:     llm.ErrorDetail{Type: "invalid_request_error", Message: strings.ReplaceAll(responsesResourceHubMessage, "Azure", resource)},
			}}
			request := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponseWebSocket)
			_, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, "synthetic", true, 4,
				func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
					state.APIKey = apiKey
					return hubRecoveryRelayTarget(state, outbound)
				},
				func(_ *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
					return recoverRejectedRemoteCompaction(outbound, adapter, executor)
				},
				func(_ *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
					return applyAzureResponsesHistoryCompatibility(outbound)
				})
			require.NoError(t, err)
			drainRejectedReasoningPipeline(t, result)
			require.Len(t, executor.continuations, 2)
			require.Len(t, executor.summaries, 1)
			require.True(t, gjson.GetBytes(executor.continuations[0], `input.#(type=="compaction")`).Exists())
			require.False(t, gjson.GetBytes(executor.continuations[1], `input.#(type=="compaction")`).Exists())
			require.Contains(t, string(executor.continuations[1]), "verified retained source summary")
		})
	}
}
