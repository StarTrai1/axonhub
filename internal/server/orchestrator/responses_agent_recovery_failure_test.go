package orchestrator

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

// A successful checkpoint/reasoning rewrite is not proof that independent
// agent ciphertext is portable. These are the successive failures reported by
// the native endpoint and the relay; neither may discard the agent's payload.
func TestResponsesRejectedAgentHydrationRetainsAllMessages(t *testing.T) {
	for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseCompact} {
		for _, raw := range []bool{false, true} {
			for _, message := range []string{
				"The encrypted content for item am_private could not be verified. Reason: encrypted content hydration failed: Encrypted content could not be decrypted or parsed.",
				"Encrypted function output content could not be decrypted or decoded.",
			} {
				t.Run(fmt.Sprintf("%s/raw=%t/%s", format, raw, message), func(t *testing.T) {
					ctx := shared.WithSessionScope(t.Context(), t.Name())
					request := agentRecoveryRequest(t, format)
					original := append([]byte(nil), request.Body...)
					owner := &ent.APIKey{ID: 781, ProjectID: 782}
					adapter := newRemoteCompactionAdapter(nil, nil, nil)
					ref, _, _, err := parseRemoteCompactionRequest(request.Body)
					require.NoError(t, err)
					adapter.summaries.SetDefault(remoteCompactionOwnerCacheKey(&PersistenceState{APIKey: owner}, remoteCompactionCacheKey(ref)), "retained checkpoint summary")
					executor := &agentRecoveryStreamExecutor{
						responsesReasoningPipelineExecutor: responsesReasoningPipelineExecutor{
							failures: []error{agentRecoveryError(rejectedReasoningMessage), agentRecoveryError(rejectedNativeCompactionMessage), agentRecoveryError(message)},
						},
						inBand: true,
					}
					state, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, "retained-agent-credential", raw, 5,
						func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
							state.APIKey = owner
							state.ChannelModelsCandidates[0].Channel.Policies.RemoteCompaction = objects.RemoteCompactionPolicyNative
							return recoverRejectedRemoteCompaction(outbound, adapter, executor)
						},
					)
					require.Error(t, err)
					require.Nil(t, result)
					require.Contains(t, err.Error(), message)
					require.Len(t, executor.requests, 3, "an unreadable agent body does not authorize another destructive retry")
					for _, attempt := range executor.requests {
						require.Contains(t, string(attempt.Body), preservedEncryptedAgentMessage)
						require.Contains(t, string(attempt.Body), preservedPlainAgentMessage)
					}
					require.Equal(t, original, request.Body)
					scope, found := responsesReasoningScope(ctx, state.CurrentCandidate.Channel, executor.requests[0])
					require.True(t, found)
					_, confirmed := rememberedResponsesReasoningRule(scope, original)
					require.False(t, confirmed, "the unsuccessful history migration must not be learned as successful")
				})
			}
		}
	}
}
