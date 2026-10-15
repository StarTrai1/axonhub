package orchestrator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
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

const rejectedRedactedCiphertextMessage = "The encrypted content gAAA...AA== could not be verified. Reason: Encrypted content could not be decrypted or parsed."

func TestResponsesRedactedEncryptionRecoveryGuards(t *testing.T) {
	body, err := sjson.SetRawBytes([]byte(responsesRejectedReasoningFixture), "input.-1", []byte(preservedReasoningCheckpoint))
	require.NoError(t, err)
	for _, tc := range []struct {
		name, message, code, param, path string
		value any
		want bool
	}{
		{name: "redacted", message: rejectedRedactedCiphertextMessage, want: true},
		{name: "whole input", message: rejectedRedactedCiphertextMessage, param: "input", want: true},
		{name: "indexed reasoning", message: rejectedRedactedCiphertextMessage, param: "input[1].encrypted_content", want: true},
		{name: "indexed checkpoint", message: rejectedRedactedCiphertextMessage, param: "input[9].encrypted_content"},
		{name: "wrong field", message: rejectedRedactedCiphertextMessage, param: "input[1].id"},
		{name: "wrong code", message: rejectedRedactedCiphertextMessage, code: "permission_denied"},
		{name: "extra text", message: rejectedRedactedCiphertextMessage + " other problem"},
		{name: "unresolved previous", message: rejectedRedactedCiphertextMessage, path: "previous_response_id", value: "resp_missing"},
		{name: "unresolved conversation", message: rejectedRedactedCiphertextMessage, path: "conversation", value: "conv_missing"},
		{name: "orphan tool", message: rejectedRedactedCiphertextMessage, path: "input.3.call_id", value: "missing"},
		{name: "multiple checkpoints", message: rejectedRedactedCiphertextMessage, path: "input.10", value: map[string]any{"type":"compaction","id":"cmp_other","encrypted_content":"other"}},
		{name: "opaque agent", message: rejectedRedactedCiphertextMessage, path: "input.10", value: json.RawMessage(preservedEncryptedAgentMessage)},
		{name: "exact ciphertext", message: strings.Replace(rejectedRedactedCiphertextMessage, "gAAA...AA==", "rejected-with-summary", 1), want: true},
		{name: "unknown ciphertext", message: strings.Replace(rejectedRedactedCiphertextMessage, "gAAA...AA==", "missing-ciphertext", 1), code: "invalid_encrypted_content"},
		{name: "conflicting ciphertext index", message: strings.Replace(rejectedRedactedCiphertextMessage, "gAAA...AA==", "rejected-with-summary", 1), code: "invalid_encrypted_content", param: "input[6].encrypted_content"},
		{name: "duplicate ciphertext", message: strings.Replace(rejectedRedactedCiphertextMessage, "gAAA...AA==", "rejected-with-summary", 1), path: "input.6.encrypted_content", value: "rejected-with-summary"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := body
			if tc.path != "" {
				changed, err = sjson.SetBytes(body, tc.path, tc.value)
				require.NoError(t, err)
			}
			_, accepted := responsesRejectedStatusRuleForDetails(changed, tc.code, tc.message, tc.param)
			require.Equal(t, tc.want, accepted)
		})
	}
}

func TestResponsesRedactedEncryptionStagedPipeline(t *testing.T) {
	for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseCompact} {
		for _, raw := range []bool{true, false} {
			for _, stage := range []string{"reasoning only", "both", "missing source", "budget", "repeated rejection"} {
				t.Run(fmt.Sprintf("%s/raw=%t/%s", format, raw, stage), func(t *testing.T) {
					ctx := shared.WithSessionScope(t.Context(), "staged-"+t.Name())
					req := rejectedReasoningPipelineRequest(t, format)
					var err error
					req.Body, err = sjson.SetRawBytes(req.Body, "input.-1", []byte(preservedReasoningCheckpoint))
					require.NoError(t, err)
					original := append([]byte(nil), req.Body...)
					ref, _, _, err := parseRemoteCompactionRequest(req.Body)
					require.NoError(t, err)
					apiKey := &ent.APIKey{ID: 511, ProjectID: 512}
					adapter := newRemoteCompactionAdapter(nil, nil, nil)
					if stage != "missing source" {
						adapter.summaries.SetDefault(remoteCompactionOwnerCacheKey(&PersistenceState{APIKey: apiKey}, remoteCompactionCacheKey(ref)), "verified retained source summary")
					}
					rejection := &httpclient.Error{StatusCode: 400, Body: []byte(`{"error":{"type":"invalid_request_error","message":"`+rejectedRedactedCiphertextMessage+`"}}`)}
					executor := &responsesReasoningPipelineExecutor{
						failures: []error{rejection}, events: rejectedReasoningCompactionEvents(),
						response: &httpclient.Response{StatusCode: http.StatusOK, Body: []byte(`{"id":"cmp_next","object":"response.compaction","output":[{"type":"compaction","id":"cmp_new","encrypted_content":"new-checkpoint"}]}`)},
					}
					if stage != "reasoning only" { executor.failures = append(executor.failures, rejection) }
					if stage == "repeated rejection" { executor.failures = append(executor.failures, rejection) }
					budget := 3
					if stage == "budget" { budget = 1 }
					state, result, processErr := runRejectedReasoningPipeline(t, ctx, req, executor, "staged-credential", raw, budget,
						func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
							state.APIKey = apiKey
							return recoverRejectedRemoteCompaction(outbound, adapter, executor)
						})
					if stage == "missing source" || stage == "budget" || stage == "repeated rejection" {
						require.Error(t, processErr)
						require.Nil(t, result)
					} else {
						require.NoError(t, processErr)
						if result.Stream { drainRejectedReasoningPipeline(t, result) }
					}
					want := 2
					if stage == "both" || stage == "repeated rejection" { want = 3 }
					require.Len(t, executor.requests, want)
					retry := executor.requests[1].Body
					require.Empty(t, encryptedResponsesReasoningHashes(retry))
					require.JSONEq(t, preservedReasoningCheckpoint, gjson.GetBytes(retry, `input.#(id=="cmp_preserved")`).Raw)
					for _, item := range gjson.GetBytes(original, "input").Array() {
						if item.Get("type").String() == "reasoning" { continue }
						expected := item.Raw
						if item.Get("id").String() == "fc_native" || item.Get("id").String() == "ctc_native" {
							expected, err = sjson.Delete(expected, "id"); require.NoError(t, err)
						}
						require.Contains(t, string(retry), expected)
					}
					if want == 3 { assertRejectedCompactionWindowPreserved(t, retry, executor.requests[2].Body) }
					require.Equal(t, original, req.Body)
					scope, ok := responsesReasoningScope(ctx, state.CurrentCandidate.Channel, executor.requests[0])
					require.True(t, ok)
					t.Cleanup(func(){ responsesReasoningRecoveries.Remove(scope) })
				})
			}
		}
	}
}
