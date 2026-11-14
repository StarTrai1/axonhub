package orchestrator

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func hubEncryptedItemMessage(id string) string {
	return "OpenAI Responses bad request: The encrypted content for item " + id + " could not be verified. Reason: Encrypted content could not be decrypted or parsed. [hub-] [trace_id=synthetic-trace]"
}

func hubEncryptedItemError(id string) *httpclient.Error {
	return &httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(fmt.Sprintf(`{"error":{"type":"invalid_request_error","message":%q}}`, hubEncryptedItemMessage(id)))}
}

func TestResponsesHubEncryptedHistoryRecovery(t *testing.T) {
	for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseWebSocket} {
		for _, raw := range []bool{false, true} {
			for _, inBand := range []bool{false, true} {
				for _, first := range []string{"cmp_native", "rs_visible"} {
					t.Run(fmt.Sprintf("%s/raw=%t/in-band=%t/first=%s", format, raw, inBand, first), func(t *testing.T) {
						ctx := shared.WithSessionScope(t.Context(), t.Name())
						req := agentRecoveryRequest(t, format)
						original := append([]byte(nil), req.Body...)
						owner := &ent.APIKey{ID: 951, ProjectID: 952}
						adapter := newRemoteCompactionAdapter(nil, nil, nil)
						ref, _, _, err := parseRemoteCompactionRequest(req.Body)
						require.NoError(t, err)
						adapter.summaries.SetDefault(remoteCompactionOwnerCacheKey(&PersistenceState{APIKey: owner}, remoteCompactionCacheKey(ref)), "authenticated hub source summary")
						failures := []error{hubEncryptedItemError("cmp_native"), hubEncryptedItemError("rs_visible")}
						if first == "rs_visible" {
							failures[0], failures[1] = failures[1], failures[0]
						}
						executor := &agentRecoveryStreamExecutor{responsesReasoningPipelineExecutor: {
							failures: failures, events: rejectedReasoningCompactionEvents(),
						}, inBand: inBand}
						state, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, t.Name(), raw, 2,
							func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
								state.APIKey = owner
								state.ChannelModelsCandidates[0].Channel.Policies.RemoteCompaction = objects.RemoteCompactionPolicyNative
								return recoverRejectedRemoteCompaction(outbound, adapter, executor)
							})
						require.NoError(t, err)
						drainRejectedReasoningPipeline(t, result)
						require.Len(t, executor.requests, 3)
						require.Equal(t, original, req.Body)
						last := executor.requests[2].Body
						require.Contains(t, string(last), "authenticated hub source summary")
						require.Empty(t, encryptedResponsesReasoningHashes(last))
						for _, value := range []string{preservedEncryptedAgentMessage, preservedPlainAgentMessage, "keep function output", "keep custom output"} {
							require.Contains(t, string(last), value)
						}
						for _, field := range []string{"prompt_cache_key", "model", "client_metadata", "reasoning"} {
							require.Equal(t, gjson.GetBytes(executor.requests[0].Body, field).Raw, gjson.GetBytes(last, field).Raw)
						}
						if first == "cmp_native" {
							require.NotEmpty(t, encryptedResponsesReasoningHashes(executor.requests[1].Body))
						} else {
							require.Equal(t, ref.EncryptedContent, gjson.GetBytes(executor.requests[1].Body, `input.#(type=="compaction").encrypted_content`).String())
						}
						scope, ok := responsesReasoningScope(ctx, state.CurrentCandidate.Channel, executor.requests[0])
						require.True(t, ok)
						t.Cleanup(func() { responsesReasoningRecoveries.Remove(scope) })
					})
				}
			}
		}
	}
}

func TestResponsesHubEncryptionStillRequiresExactRejection(t *testing.T) {
	body := agentRecoveryRequest(t, llm.APIFormatOpenAIResponse).Body
	ref, _, _, err := parseRemoteCompactionRequest(body)
	require.NoError(t, err)
	for _, id := range []string{"cmp_native", "rs_visible"} {
		for _, tc := range []struct {
			name, message, code, param string
			want                       bool
		}{
			{name: "exact hub", message: hubEncryptedItemMessage(id), want: true},
			{name: "unknown wrapper", message: strings.ReplaceAll(hubEncryptedItemMessage(id), "[hub-]", "[hub-other]")},
			{name: "duplicate wrapper", message: strings.ReplaceAll(hubEncryptedItemMessage(id), "[hub-]", "[hub-] [hub-]")},
			{name: "extra prose", message: hubEncryptedItemMessage(id) + " try anything"},
			{name: "other item", message: hubEncryptedItemMessage(id + "_foreign")},
			{name: "conflicting parameter", message: hubEncryptedItemMessage(id), param: "input[999].encrypted_content"},
			{name: "other error", message: hubEncryptedItemMessage(id), code: "rate_limit_exceeded"},
		} {
			t.Run(id+"/"+tc.name, func(t *testing.T) {
				if id == "cmp_native" {
					require.Equal(t, tc.want, responsesRejectedCompactionMessage(body, ref, tc.code, tc.message, tc.param))
				} else {
					_, accepted := responsesRejectedReasoningMessageRule(body, tc.code, tc.message, tc.param)
					require.Equal(t, tc.want, accepted)
				}
			})
		}
	}
}
