package orchestrator

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

const responsesRejectedReasoningFixture = `{
	"model":"gpt-5.6-sol",
	"prompt_cache_key":"keep-cache-key",
	"input":[
		{"type":"message","id":"msg_user","role":"user","content":[{"type":"input_text","text":"keep explicit context"}],"internal_chat_message_metadata_passthrough":{"content_item_kinds":[]}},
		{"type":"reasoning","id":"rs_visible","encrypted_content":"rejected-with-summary","summary":[{"type":"summary_text","text":"visible summary"}],"content":[{"type":"reasoning_text","text":"visible rationale"}]},
		{"type":"function_call","id":"fc_native","call_id":"call_function","name":"exec","arguments":"{}"},
		{"type":"function_call_output","call_id":"call_function","output":"keep function output"},
		{"type":"custom_tool_call","id":"ctc_native","call_id":"call_custom","name":"custom","input":"keep custom input"},
		{"type":"custom_tool_call_output","call_id":"call_custom","output":"keep custom output"},
		{"type":"reasoning","id":"rs_private","encrypted_content":"rejected-without-summary","summary":[]},
		{"type":"reasoning","id":"rs_public","summary":[{"type":"summary_text","text":"unrejected summary"}]},
		{"type":"message","id":"msg_assistant","role":"assistant","content":[{"type":"output_text","text":"visible answer"}]}
	]
}`

func TestResponsesRejectedReasoningRecoveryGuards(test *testing.T) {
	for _, scenario := range []struct {
		name   string
		path   string
		value  any
		remove bool
		param  string
		want   bool
	}{
		{name: "full explicit history", want: true},
		{name: "indexed encrypted reasoning", param: "input[1].encrypted_content", want: true},
		{name: "untyped user message", path: "input.0.type", remove: true, want: true},
		{name: "string user message", path: "input.0.content", value: "history", want: true},
		{name: "unresolved previous response", path: "previous_response_id", value: "resp_missing"},
		{name: "no user history", path: "input.0.role", value: "assistant"},
		{name: "empty user content", path: "input.0.content", value: ""},
		{name: "empty user content array", path: "input.0.content", value: []any{}},
		{name: "null user content", path: "input.0.content", value: nil},
		{name: "non-object item", path: "input.9", value: "not an input item"},
		{name: "wrong parameter type", param: "input[2].encrypted_content"},
		{name: "unencrypted reasoning parameter", param: "input[7].encrypted_content"},
		{name: "missing indexed ciphertext", path: "input.1.encrypted_content", remove: true, param: "input[1].encrypted_content"},
		{name: "out-of-range parameter", param: "input[9999].encrypted_content"},
		{name: "negative parameter", param: "input[-1].encrypted_content"},
		{name: "overflowing parameter", param: "input[99999999999999999999999].encrypted_content"},
		{name: "nested parameter", param: "input[1].encrypted_content.extra"},
		{name: "unrelated parameter", param: "input"},
		{name: "missing function call", path: "input.2", remove: true},
		{name: "orphan function output", path: "input.3.call_id", value: "call_missing"},
		{name: "orphan custom output", path: "input.5.call_id", value: "call_missing"},
		{name: "mismatched tool type", path: "input.5.type", value: "function_call_output"},
		{name: "encrypted function arguments", path: "input.2.encrypted_function_args", value: "opaque"},
		{name: "encrypted message", path: "input.0.encrypted_content", value: "opaque"},
		{name: "non-string ciphertext", path: "input.1.encrypted_content", value: 123},
		{name: "unknown input type", path: "input.9", value: map[string]any{"type": "future_opaque_state"}},
		{name: "native compaction", path: "input.9", value: map[string]any{"type": "compaction", "encrypted_content": "opaque"}},
		{name: "compaction summary", path: "input.9", value: map[string]any{"type": "compaction_summary"}},
		{name: "context compaction", path: "input.9", value: map[string]any{"type": "context_compaction"}},
		{name: "unresolved item reference", path: "input.9", value: map[string]any{"type": "item_reference", "id": "rs_missing"}},
		{name: "encrypted agent message", path: "input.9", value: map[string]any{"type": "agent_message", "content": []any{map[string]any{"type": "encrypted_content", "encrypted_content": "opaque"}}}},
		{name: "plaintext agent message", path: "input.9", value: map[string]any{"type": "agent_message", "content": []any{map[string]any{"type": "output_text", "text": "visible"}}}, want: true},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			body := []byte(responsesRejectedReasoningFixture)
			var err error
			if scenario.path != "" {
				if scenario.remove {
					body, err = sjson.DeleteBytes(body, scenario.path)
				} else {
					body, err = sjson.SetBytes(body, scenario.path, scenario.value)
				}
				require.NoError(test, err)
			}
			rule, accepted := responsesRejectedReasoningRule(body, scenario.param)
			require.Equal(test, scenario.want, accepted)
			if accepted {
				require.True(test, rule.dropItem)
				require.Equal(test, "reasoning", rule.itemType)
				require.Equal(test, "encrypted_content", rule.fieldName())
			}
		})
	}
	for _, body := range []string{`{`, `{"input":"delta"}`, `{"input":[]}`, `{"input":[{"role":"user","content":"no encrypted reasoning"}]}`} {
		_, accepted := responsesRejectedReasoningRule([]byte(body), "")
		require.False(test, accepted)
	}
}

func TestResponsesRejectedReasoningCompatibilityPreservesExplicitHistory(test *testing.T) {
	outbound := newCodexResponsesPassThroughOutbound()
	outbound.state.CurrentCandidate.Channel.ID = 93000
	request := outbound.state.RawProviderRequest
	request.URL = "https://reasoning-recovery.example/v1/responses"
	request.Body = []byte(responsesRejectedReasoningFixture)
	middleware := applyResponsesRejectedStatusCompatibility(outbound)

	initial, err := middleware.OnOutboundRawRequest(test.Context(), request)
	require.NoError(test, err)
	require.Equal(test, responsesRejectedReasoningFixture, string(initial.Body))
	providerErr := &httpclient.Error{
		StatusCode: http.StatusBadRequest,
		Body:       []byte(`{"error":{"code":"invalid_encrypted_content"}}`),
	}
	middleware.OnOutboundRawError(test.Context(), providerErr)
	require.True(test, outbound.CanRetry(providerErr))
	require.NoError(test, outbound.PrepareForRetry(test.Context()))
	retry, err := middleware.OnOutboundRawRequest(test.Context(), request)
	require.NoError(test, err)
	require.Equal(test, int64(8), gjson.GetBytes(retry.Body, "input.#").Int())
	require.Equal(test, "visible summary\n\nvisible rationale", gjson.GetBytes(retry.Body, "input.1.content.0.text").String())
	require.Equal(test, "assistant", gjson.GetBytes(retry.Body, "input.1.role").String())
	require.Equal(test, "message", gjson.GetBytes(retry.Body, "input.1.type").String())
	require.False(test, gjson.GetBytes(retry.Body, "input.1.id").Exists())
	require.Empty(test, gjson.GetBytes(retry.Body, "input.#(encrypted_content)#").Array())
	for _, path := range []string{"model", "prompt_cache_key", "input.0", "input.2", "input.3", "input.4", "input.5"} {
		require.JSONEq(test, gjson.Get(responsesRejectedReasoningFixture, path).Raw, gjson.GetBytes(retry.Body, path).Raw)
	}
	require.JSONEq(test, gjson.Get(responsesRejectedReasoningFixture, "input.7").Raw, gjson.GetBytes(retry.Body, "input.6").Raw)
	require.JSONEq(test, gjson.Get(responsesRejectedReasoningFixture, "input.8").Raw, gjson.GetBytes(retry.Body, "input.7").Raw)
	middleware.OnOutboundRawError(test.Context(), providerErr)
	require.False(test, hasResponsesRejectedStatusCompatibilityRetry(outbound.state, 93000))
	rewritten, changed, err := stripResponsesRejectedStatus(retry.Body, outbound.state.responsesRejectedStatusRules[93000])
	require.NoError(test, err)
	require.False(test, changed)
	require.Equal(test, retry.Body, rewritten)

	fresh := newCodexResponsesPassThroughOutbound()
	fresh.state.CurrentCandidate.Channel.ID = 93000
	fresh.state.RawProviderRequest.URL = request.URL
	fresh.state.RawProviderRequest.Body = []byte(responsesRejectedReasoningFixture)
	untouched, err := applyResponsesRejectedStatusCompatibility(fresh).OnOutboundRawRequest(test.Context(), fresh.state.RawProviderRequest)
	require.NoError(test, err)
	require.Equal(test, responsesRejectedReasoningFixture, string(untouched.Body))

	outbound.state.CurrentCandidate.Channel.ID = 93001
	request.Body = []byte(responsesRejectedReasoningFixture)
	untouched, err = middleware.OnOutboundRawRequest(test.Context(), request)
	require.NoError(test, err)
	require.Equal(test, responsesRejectedReasoningFixture, string(untouched.Body))
}

func TestResponsesRejectedReasoningCompatibilityRequiresExplicitCodexRejection(test *testing.T) {
	for _, scenario := range []struct {
		name       string
		channel    entchannel.Type
		status     int
		apiFormat  llm.APIFormat
		errorBody  string
		compatible bool
	}{
		{"explicit Codex rejection", entchannel.TypeCodex, 400, llm.APIFormatOpenAIResponse, `{"error":{"code":"invalid_encrypted_content"}}`, true},
		{"indexed Codex rejection", entchannel.TypeCodex, 400, llm.APIFormatOpenAIResponse, `{"error":{"code":"invalid_encrypted_content","param":"input[1].encrypted_content"}}`, true},
		{"generic validation failure", entchannel.TypeCodex, 400, llm.APIFormatOpenAIResponse, `{"error":{"code":"invalid_responses_request","message":"invalid codex request"}}`, false},
		{"message without explicit code", entchannel.TypeCodex, 400, llm.APIFormatOpenAIResponse, `{"error":{"message":"invalid_encrypted_content"}}`, false},
		{"upstream server error", entchannel.TypeCodex, 500, llm.APIFormatOpenAIResponse, `{"error":{"code":"invalid_encrypted_content"}}`, false},
		{"non-Codex Responses channel", entchannel.TypeOpenaiResponses, 400, llm.APIFormatOpenAIResponse, `{"error":{"code":"invalid_encrypted_content"}}`, false},
		{"non-Responses format", entchannel.TypeCodex, 400, llm.APIFormatOpenAIChatCompletion, `{"error":{"code":"invalid_encrypted_content"}}`, false},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			outbound := newCodexResponsesPassThroughOutbound()
			outbound.state.CurrentCandidate.Channel.Type = scenario.channel
			outbound.state.RawProviderRequest.APIFormat = string(scenario.apiFormat)
			outbound.state.RawProviderRequest.Body = []byte(responsesRejectedReasoningFixture)
			providerErr := &httpclient.Error{StatusCode: scenario.status, Body: []byte(scenario.errorBody)}
			applyResponsesRejectedStatusCompatibility(outbound).OnOutboundRawError(test.Context(), providerErr)
			require.Equal(test, scenario.compatible, hasResponsesRejectedStatusCompatibilityRetry(outbound.state, outbound.state.CurrentCandidate.Channel.ID))
		})
	}
}

func TestResponsesRejectedReasoningRecoveryFailsClosedForChangedInput(test *testing.T) {
	body := []byte(responsesRejectedReasoningFixture)
	rule, accepted := responsesRejectedReasoningRule(body, "")
	require.True(test, accepted)
	for _, scenario := range []struct {
		name  string
		path  string
		value any
	}{
		{"unresolved previous response", "previous_response_id", "resp_missing"},
		{"unknown summary type", "input.1.summary.0.type", "future_summary"},
		{"unknown content type", "input.1.content.0.type", "future_content"},
		{"malformed summary", "input.1.summary", "not an array"},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			changed, err := sjson.SetBytes(body, scenario.path, scenario.value)
			require.NoError(test, err)
			result, rewritten, err := stripResponsesRejectedStatus(changed, []responsesRejectedStatusRule{rule})
			require.Error(test, err)
			require.False(test, rewritten)
			require.Nil(test, result)
			require.True(test, json.Valid(changed))
		})
	}
}
