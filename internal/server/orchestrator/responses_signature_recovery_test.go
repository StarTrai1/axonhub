package orchestrator

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func relayEncryptedContentError(message string) error {
	return &httpclient.Error{
		StatusCode: http.StatusBadRequest,
		Body:       []byte(`{"error":{"code":"thinking_signature_invalid","type":"invalid_request_error","message":"` + message + `"}}`),
	}
}

func TestResponsesRejectedRelaySignaturePipeline(t *testing.T) {
	for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseCompact} {
		for _, passThrough := range []bool{true, false} {
			for _, scenario := range []string{"reasoning", "reasoning with checkpoint", "checkpoint then reasoning"} {
				name := string(format) + map[bool]string{true: "/raw/", false: "/converted/"}[passThrough] + scenario
				t.Run(name, func(t *testing.T) {
					ctx := shared.WithSessionScope(t.Context(), "relay-"+t.Name())
					request := rejectedReasoningPipelineRequest(t, format)
					adapter := newRemoteCompactionAdapter(nil, nil, nil)
					apiKey := &ent.APIKey{ID: 401, ProjectID: 402}
					executor := &responsesReasoningPipelineExecutor{
						failures: []error{relayEncryptedContentError(rejectedReasoningMessage)},
						events:   rejectedReasoningCompactionEvents(),
						response: &httpclient.Response{StatusCode: http.StatusOK, Body: []byte(`{"id":"cmp_next","object":"response.compaction","output":[{"type":"compaction","id":"cmp_new","encrypted_content":"new-checkpoint"}]}`)},
					}
					var err error
					request.Body, err = sjson.SetBytes(request.Body, "client_metadata.thread_id", request.Headers.Get("Thread-Id"))
					require.NoError(t, err)
					if scenario != "reasoning" {
						request.Body, err = sjson.SetRawBytes(request.Body, "input.-1", []byte(preservedReasoningCheckpoint))
						require.NoError(t, err)
					}
					if scenario == "checkpoint then reasoning" {
						ref, _, _, parseErr := parseRemoteCompactionRequest(request.Body)
						require.NoError(t, parseErr)
						adapter.summaries.SetDefault(remoteCompactionOwnerCacheKey(&PersistenceState{APIKey: apiKey}, remoteCompactionCacheKey(ref)), "verified retained source summary")
						executor.failures = append([]error{relayEncryptedContentError("The encrypted content for item cmp_preserved could not be verified. Reason: Encrypted content could not be decrypted or parsed.")}, executor.failures...)
					}
					original := append([]byte(nil), request.Body...)
					compactionRecovery := func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
						state.APIKey = apiKey
						state.ChannelModelsCandidates[0].Channel.Policies.RemoteCompaction = objects.RemoteCompactionPolicyNative
						return recoverRejectedRemoteCompaction(outbound, adapter, executor)
					}
					state, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, "relay-credential", passThrough, len(executor.failures), compactionRecovery)
					require.NoError(t, err)
					if result.Stream {
						drainRejectedReasoningPipeline(t, result)
					}
					require.Len(t, executor.requests, len(executor.failures)+1)
					retry := executor.requests[len(executor.requests)-1].Body
					require.Empty(t, encryptedResponsesReasoningHashes(retry))
					require.Contains(t, string(retry), "visible summary")
					require.Contains(t, string(retry), "visible rationale")
					require.Equal(t, original, request.Body)
					for _, item := range gjson.GetBytes(original, "input").Array() {
						if item.Get("type").String() == "reasoning" {
							continue
						}
						if scenario == "checkpoint then reasoning" && item.Get("type").String() == "compaction" {
							require.Contains(t, string(retry), "verified retained source summary")
							require.NotContains(t, string(retry), "opaque-native-checkpoint")
							continue
						}
						expected := item.Raw
						if item.Get("id").String() == "fc_native" || item.Get("id").String() == "ctc_native" {
							var err error
							expected, err = sjson.Delete(expected, "id")
							require.NoError(t, err)
						}
						require.Contains(t, string(retry), expected, "retain messages, tool pairs, controls and unrejected checkpoints")
					}
					for _, field := range []string{"model", "reasoning", "prompt_cache_key", "client_metadata", "store"} {
						require.Equal(t, gjson.GetBytes(executor.requests[0].Body, field).Raw, gjson.GetBytes(retry, field).Raw, field)
					}
					if scenario == "checkpoint then reasoning" {
						require.NotEmpty(t, encryptedResponsesReasoningHashes(executor.requests[1].Body), "checkpoint rejection alone must preserve reasoning")
					}

					// Only successful recovery can be reused. Fresh target reasoning
					// must survive the next request even with no retry budget.
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
					_, result, err = runRejectedReasoningPipeline(t, ctx, request, next, "relay-credential", passThrough, 0, compactionRecovery)
					require.NoError(t, err)
					if result.Stream {
						drainRejectedReasoningPipeline(t, result)
					}
					require.Len(t, next.requests, 1)
					require.Len(t, encryptedResponsesReasoningHashes(next.requests[0].Body), 1)
					require.Equal(t, "new-target-reasoning", gjson.GetBytes(next.requests[0].Body, `input.#(id=="rs_target_new").encrypted_content`).String())
				})
			}
		}
	}
}

func TestResponsesRejectedRelaySignatureDoesNotBypassBudgetOrSource(t *testing.T) {
	for _, scenario := range []string{"no retries", "missing source", "generic signature error"} {
		t.Run(scenario, func(t *testing.T) {
			request := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
			original := append([]byte(nil), request.Body...)
			failure := relayEncryptedContentError(rejectedNativeCompactionMessage)
			retries := 1
			switch scenario {
			case "no retries":
				retries = 0
			case "generic signature error":
				failure = relayEncryptedContentError("Invalid thinking signature")
			}
			executor := &responsesReasoningPipelineExecutor{failures: []error{failure}}
			_, result, err := runRejectedReasoningPipeline(t, shared.WithSessionScope(t.Context(), t.Name()), request, executor, "relay-credential", true, retries,
				func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
					state.APIKey = &ent.APIKey{ID: 403, ProjectID: 404}
					return recoverRejectedRemoteCompaction(outbound, newRemoteCompactionAdapter(nil, nil, nil), executor)
				})
			require.Error(t, err)
			require.Nil(t, result)
			require.Len(t, executor.requests, 1)
			require.Equal(t, original, request.Body)
			if scenario == "missing source" {
				require.ErrorContains(t, err, "request history is unavailable")
			}
		})
	}
}
