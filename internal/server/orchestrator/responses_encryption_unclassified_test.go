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

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

// Preserve the observed relay envelope, with synthetic ciphertext and trace ID.
const rejectedUnclassifiedCiphertextMessage = "The encrypted content gAAA...vlah could not be verified. Reason: Encrypted content could not be decrypted or parsed. [trace_id=synthetic-trace code=30000]"

func TestResponsesUnclassifiedEncryptionGuards(t *testing.T) {
	for _, tc := range []struct {
		name, message, code, param, path string
		value                            any
		want                             bool
	}{
		{name: "observed envelope", want: true},
		{name: "whole input", param: "input", want: true},
		{name: "indexed reasoning", param: "input[1].encrypted_content", want: true},
		{name: "code alias alone with exact message", message: rejectedRedactedCiphertextMessage, want: true},
		{name: "trailer alone with known code", code: "validation_error", want: true},
		{name: "wrong code", code: "permission_denied"},
		{name: "generic unclassified", message: "Invalid request"},
		{name: "content refusal", message: "content-blocked (request id: synthetic-refusal)"},
		{name: "other numeric code", message: strings.Replace(rejectedUnclassifiedCiphertextMessage, "30000", "42900", 1)},
		{name: "bare numeric trailer", message: strings.Replace(rejectedUnclassifiedCiphertextMessage, "trace_id=synthetic-trace ", "", 1)},
		{name: "duplicate numeric code", message: strings.Replace(rejectedUnclassifiedCiphertextMessage, "code=30000", "code=30000 code=30000", 1)},
		{name: "extra prose", message: rejectedUnclassifiedCiphertextMessage + " other problem"},
		{name: "wrong parameter", param: "input[1].id"},
		{name: "plain message parameter", param: "input[0].encrypted_content"},
		{name: "unknown ciphertext", message: strings.Replace(rejectedUnclassifiedCiphertextMessage, "gAAA...vlah", "unknown-ciphertext", 1)},
		{name: "unresolved previous", path: "previous_response_id", value: "resp_missing"},
		{name: "unresolved conversation", path: "conversation", value: "conv_missing"},
		{name: "orphan tool output", path: "input.3.call_id", value: "missing"},
		{name: "opaque agent", path: "input.-1", value: json.RawMessage(preservedEncryptedAgentMessage)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(responsesRejectedReasoningFixture)
			if tc.path != "" {
				var err error
				body, err = sjson.SetBytes(body, tc.path, tc.value)
				require.NoError(t, err)
			}
			message, code := tc.message, tc.code
			if message == "" {
				message = rejectedUnclassifiedCiphertextMessage
			}
			if code == "" {
				code = "invalid_request_unclassified"
			}
			_, accepted := responsesRejectedStatusRuleForDetails(body, code, message, tc.param)
			require.Equal(t, tc.want, accepted)
		})
	}
}

func TestResponsesUnclassifiedEncryptionContinuation(t *testing.T) {
	for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseWebSocket} {
		for _, raw := range []bool{true, false} {
			for _, inBand := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/raw=%t/in-band=%t", format, raw, inBand), func(t *testing.T) {
					ctx := shared.WithSessionScope(t.Context(), t.Name())
					request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse)
					request.APIFormat = string(format)
					original := append([]byte(nil), request.Body...)
					executor := &agentRecoveryStreamExecutor{inBand: inBand}
					executor.failures = []error{&httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"type":"invalid_request_error","code":"invalid_request_unclassified","message":"` + rejectedUnclassifiedCiphertextMessage + `"}}`)}}
					executor.events = rejectedReasoningCompactionEvents()
					state, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, t.Name(), raw, 1)
					require.NoError(t, err)
					events := drainRejectedReasoningPipeline(t, result)
					require.Len(t, executor.requests, 2, "recover on the same channel once")
					require.Contains(t, events, "response.completed")
					require.Equal(t, original, request.Body)
					initial, retry := executor.requests[0].Body, executor.requests[1].Body
					require.NotEmpty(t, encryptedResponsesReasoningHashes(initial))
					require.Empty(t, encryptedResponsesReasoningHashes(retry))
					require.Equal(t, "visible summary\n\nvisible rationale", gjson.GetBytes(retry, "input.2.content.0.text").String())
					for _, path := range []string{"model", "reasoning", "prompt_cache_key", "input.0", "input.1", "input.4", "input.6"} {
						require.JSONEq(t, gjson.GetBytes(initial, path).Raw, gjson.GetBytes(retry, path).Raw, path)
					}
					for _, path := range []string{"input.3", "input.5"} {
						expected, err := sjson.Delete(gjson.GetBytes(initial, path).Raw, "id")
						require.NoError(t, err)
						require.JSONEq(t, expected, gjson.GetBytes(retry, path).Raw)
					}
					scope, ok := responsesReasoningScope(ctx, state.CurrentCandidate.Channel, executor.requests[0])
					require.True(t, ok)
					t.Cleanup(func() { responsesReasoningRecoveries.Remove(scope) })
				})
			}
		}
	}
}

func TestResponsesUnclassifiedEncryptionStagedPipeline(t *testing.T) {
	testResponsesStagedEncryptionPipeline(t, rejectedUnclassifiedCiphertextMessage, "invalid_request_unclassified")
}
