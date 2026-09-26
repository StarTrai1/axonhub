package orchestrator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

const rejectedReasoningMessage = "The encrypted content for item rs_visible could not be verified. Reason: Encrypted content could not be decrypted or parsed."

func TestResponsesRejectedReasoningRecognizesExactRelayMessage(t *testing.T) {
	for _, tt := range []struct {
		name    string
		message string
		code    string
		param   string
		status  int
		want    bool
	}{
		{name: "unwrapped", message: rejectedReasoningMessage, status: 400, want: true},
		{name: "agent relay wrapper", message: "OpenAI Responses bad request: " + rejectedReasoningMessage + " [trace_id=abc123]", status: 400, want: true},
		{name: "generic bad request code", message: rejectedReasoningMessage, code: "bad_request", status: 400, want: true},
		{name: "relay signature code", message: rejectedReasoningMessage, code: "thinking_signature_invalid", status: 400, want: true},
		{name: "relay signature indexed", message: rejectedReasoningMessage, code: "thinking_signature_invalid", param: "input[1].encrypted_content", status: 400, want: true},
		{name: "relay signature without exact message", message: "Invalid thinking signature", code: "thinking_signature_invalid", param: "input[1].encrypted_content", status: 400},
		{name: "relay signature unknown item", message: strings.ReplaceAll(rejectedReasoningMessage, "rs_visible", "rs_unknown"), code: "thinking_signature_invalid", status: 400},
		{name: "relay signature wrong index", message: rejectedReasoningMessage, code: "thinking_signature_invalid", param: "input[6].encrypted_content", status: 400},
		{name: "relay signature server error", message: rejectedReasoningMessage, code: "thinking_signature_invalid", status: 500},
		{name: "matching parameter", message: rejectedReasoningMessage, param: "input[1].encrypted_content", status: 400, want: true},
		{name: "unknown item", message: strings.ReplaceAll(rejectedReasoningMessage, "rs_visible", "rs_unknown"), status: 400},
		{name: "mismatched parameter", message: rejectedReasoningMessage, param: "input[6].encrypted_content", status: 400},
		{name: "unrelated code", message: rejectedReasoningMessage, code: "invalid_model", status: 400},
		{name: "server error", message: rejectedReasoningMessage, status: 500},
		{name: "generic encryption mention", message: "invalid_encrypted_content", status: 400},
		{name: "truncated rejection", message: "The encrypted content for item rs_visible could not be verified.", status: 400},
		{name: "quoted example", message: "Example error: " + rejectedReasoningMessage, status: 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"error": map[string]any{"message": tt.message, "code": tt.code, "param": tt.param}})
			require.NoError(t, err)
			for _, failure := range []error{
				&httpclient.Error{StatusCode: tt.status, Body: body},
				&llm.ResponseError{StatusCode: tt.status, Detail: llm.ErrorDetail{Message: tt.message, Code: tt.code, Param: tt.param}},
			} {
				outbound := newCodexResponsesPassThroughOutbound()
				outbound.state.RawProviderRequest.Body = []byte(responsesRejectedReasoningFixture)
				applyResponsesRejectedStatusCompatibility(outbound).OnOutboundRawError(t.Context(), failure)
				require.Equal(t, tt.want, hasResponsesRejectedStatusCompatibilityRetry(outbound.state, 1))
			}
		})
	}
	_, accepted := responsesRejectedStatusRuleFromError(&httpclient.Error{StatusCode: 400, Body: []byte(rejectedReasoningMessage)}, []byte(responsesRejectedReasoningFixture))
	require.True(t, accepted)
	duplicated, err := sjson.SetBytes([]byte(responsesRejectedReasoningFixture), "input.6.id", "rs_visible")
	require.NoError(t, err)
	_, accepted = responsesRejectedReasoningMessageRule(duplicated, "", rejectedReasoningMessage, "")
	require.False(t, accepted, "the reported item must be unique")
}

func TestResponsesRejectedReasoningPreservesCompactionTriggerAndTools(t *testing.T) {
	items := []json.RawMessage{
		json.RawMessage(`{"type":"additional_tools","tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"run","async":true}]}]}`),
		json.RawMessage(`{"type":"configuration_update","reasoning":{"effort":"high"}}`),
	}
	for _, item := range gjson.Get(responsesRejectedReasoningFixture, "input").Array() {
		items = append(items, json.RawMessage(item.Raw))
	}
	items = append(items, json.RawMessage(`{"type":"compaction_trigger"}`))
	body, err := sjson.SetBytes([]byte(responsesRejectedReasoningFixture), "input", items)
	require.NoError(t, err)
	outbound := newCodexResponsesPassThroughOutbound()
	outbound.state.RawProviderRequest.Body = body
	middleware := applyResponsesRejectedStatusCompatibility(outbound)
	failure := &llm.ResponseError{StatusCode: 400, Detail: llm.ErrorDetail{Code: "invalid_encrypted_content"}}
	middleware.OnOutboundRawError(t.Context(), failure)
	require.True(t, outbound.CanRetry(failure))
	require.NoError(t, outbound.PrepareForRetry(t.Context()))
	request := *outbound.state.RawProviderRequest
	result, err := middleware.OnOutboundRawRequest(t.Context(), &request)
	require.NoError(t, err)
	input := gjson.GetBytes(result.Body, "input").Array()
	require.JSONEq(t, string(items[0]), input[0].Raw)
	require.JSONEq(t, string(items[1]), input[1].Raw)
	require.JSONEq(t, `{"type":"compaction_trigger"}`, input[len(input)-1].Raw)
	require.Equal(t, "visible summary\n\nvisible rationale", input[3].Get("content.0.text").String())
	require.Empty(t, gjson.GetBytes(result.Body, "input.#(encrypted_content)#").Array())
	for index := 2; index <= 5; index++ {
		expected, err := sjson.Delete(gjson.Get(responsesRejectedReasoningFixture, "input").Array()[index].Raw, "id")
		require.NoError(t, err)
		require.JSONEq(t, expected, input[index+2].Raw)
	}
	require.Equal(t, "keep-cache-key", gjson.GetBytes(result.Body, "prompt_cache_key").String())
	require.NotEmpty(t, gjson.GetBytes(body, "input.3.encrypted_content").String(), "the client's source request must stay intact")
	middleware.OnOutboundRawError(t.Context(), failure)
	require.False(t, outbound.CanRetry(failure), "a repeated rejection must not loop")
}
