package orchestrator

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestCheckpointRelayResourceRejectionRecoversOriginalConversation(t *testing.T) {
	for _, message := range []string{responsesResourceRequestIDMessage, responsesResourceRedactedMessage} {
		for _, checkpoint := range []bool{false, true} {
			for _, inBand := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/checkpoint=%t/in-band=%t", message, checkpoint, inBand), func(t *testing.T) {
					ctx := shared.WithSessionScope(t.Context(), "relay-resource-"+t.Name())
					req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
					adapter := newRemoteCompactionAdapter(nil, nil, nil)
					apiKey := &ent.APIKey{ID: 601, ProjectID: 602}
					ref, _, _, err := parseRemoteCompactionRequest(req.Body)
					require.NoError(t, err)
					adapter.summaries.SetDefault(remoteCompactionOwnerCacheKey(&PersistenceState{APIKey: apiKey}, remoteCompactionCacheKey(ref)), "verified retained source summary")
					failure := &httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"type":"invalid_request_error","code":"bad_request","message":"` + message + `"}}`)}
					failures := []error{failure}
					if !checkpoint {
						req.Body, err = sjson.DeleteBytes([]byte(responsesResourceHistoryFixture), "input.8")
						require.NoError(t, err)
						failures = append(failures, failure)
					}
					original := append([]byte(nil), req.Body...)
					executor := &agentRecoveryStreamExecutor{responsesReasoningPipelineExecutor: responsesReasoningPipelineExecutor{
						failures: failures, events: rejectedReasoningCompactionEvents(),
					}, inBand: inBand}
					state, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "synthetic-relay-resource-key", true, len(failures),
						func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
							state.APIKey = apiKey
							return recoverRejectedRemoteCompaction(outbound, adapter, executor)
						})
					require.NoError(t, err)
					events := drainRejectedReasoningPipeline(t, result)
					require.Contains(t, events, "response.completed")
					require.Len(t, executor.requests, len(failures)+1)
					require.Equal(t, original, req.Body)
					for _, attempt := range executor.requests {
						require.Equal(t, executor.requests[0].URL, attempt.URL)
						require.Equal(t, executor.requests[0].Headers.Get("Authorization"), attempt.Headers.Get("Authorization"))
						require.Equal(t, req.Headers.Get("Thread-Id"), attempt.Headers.Get("Thread-Id"))
					}
					if checkpoint {
						assertRejectedCompactionWindowPreserved(t, executor.requests[0].Body, executor.requests[1].Body)
						return
					}
					assertPortableIDsOnlyRemoved(t, executor.requests[0].Body, executor.requests[1].Body)
					last := executor.requests[2].Body
					require.Empty(t, encryptedResponsesReasoningHashes(last))
					require.Equal(t, "visible reasoning summary", gjson.GetBytes(last, "input.2.content.0.text").String())
					for _, path := range []string{"input.0", "input.1", "input.3", "input.4", "input.5", "input.6", "input.7", "reasoning", "prompt_cache_key"} {
						require.JSONEq(t, gjson.GetBytes(executor.requests[1].Body, path).Raw, gjson.GetBytes(last, path).Raw, path)
					}
					scope, ok := responsesReasoningScope(ctx, state.CurrentCandidate.Channel, executor.requests[0])
					require.True(t, ok)
					t.Cleanup(func() { responsesReasoningRecoveries.Remove(scope) })
				})
			}
		}
	}
}
