package orchestrator

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestResponsesRejectedMetadataHandlesCodexToolResults(test *testing.T) {
	for _, itemType := range []string{
		"message", "agent_message", "reasoning", "local_shell_call", "function_call", "function_call_output",
		"custom_tool_call", "custom_tool_call_output", "tool_search_call", "tool_search_output", "web_search_call",
		"image_generation_call", "compaction", "context_compaction",
	} {
		test.Run(itemType, func(test *testing.T) {
			body, err := json.Marshal(map[string]any{
				"model": "gpt-6-astra",
				"input": []any{map[string]any{
					"type": itemType, "id": "item_preserved", "call_id": "call_preserved", "output": "tool result",
					"encrypted_content": "opaque-preserved", "async": true,
					"internal_chat_message_metadata_passthrough": map[string]any{"cell_id": "cell", "tool_calls_complete": true},
				}},
			})
			require.NoError(test, err)
			failure := &llm.ResponseError{StatusCode: http.StatusBadRequest, Detail: llm.ErrorDetail{
				Code: "unknown_parameter", Param: "input[0].internal_chat_message_metadata_passthrough.cell_id",
			}}
			rule, accepted := responsesRejectedStatusRuleFromError(failure, body)
			require.True(test, accepted)
			require.True(test, hasResponsesInternalMetadata(body))
			rewritten, changed, err := stripResponsesRejectedStatus(body, []responsesRejectedStatusRule{rule})
			require.NoError(test, err)
			require.True(test, changed)
			expected, err := sjson.DeleteBytes(body, "input.0.internal_chat_message_metadata_passthrough")
			require.NoError(test, err)
			require.JSONEq(test, string(expected), string(rewritten))
			require.True(test, gjson.GetBytes(body, "input.0.internal_chat_message_metadata_passthrough.cell_id").Exists())
		})
	}
}

func TestResponsesRejectedMetadataPersistsForToolOnlyHistory(test *testing.T) {
	client, request, outbound := responsesMetadataPersistenceFixture(test)
	request.Body = []byte(`{"model":"gpt-6-astra","input":[{"type":"custom_tool_call_output","call_id":"call_native","output":"preserve result","internal_chat_message_metadata_passthrough":{"cell_id":"cell","tool_calls_complete":true}}]}`)
	key := responsesMetadataKey(1729, request)
	test.Cleanup(func() { clearResponsesMetadataTestCache(key) })
	applyResponsesRejectedStatusCompatibility(outbound).OnOutboundRawError(test.Context(), &httpclient.Error{
		StatusCode: http.StatusBadRequest,
		Body:       []byte(`{"error":{"code":"unknown_parameter","param":"input[0].internal_chat_message_metadata_passthrough.cell_id"}}`),
	})
	require.True(test, hasResponsesRejectedStatusCompatibilityRetry(outbound.state, 1729))
	clearResponsesMetadataTestCache(key)
	restarted := &PersistentOutboundTransformer{state: &PersistenceState{
		SystemService:    biz.NewSystemService(biz.SystemServiceParams{Ent: client}),
		CurrentCandidate: outbound.state.CurrentCandidate,
	}}
	outgoing := *request
	result, err := applyResponsesRejectedStatusCompatibility(restarted).OnOutboundRawRequest(test.Context(), &outgoing)
	require.NoError(test, err)
	expected, err := sjson.DeleteBytes(request.Body, "input.0.internal_chat_message_metadata_passthrough")
	require.NoError(test, err)
	require.JSONEq(test, string(expected), string(result.Body))
	require.False(test, hasResponsesRejectedStatusCompatibilityRetry(restarted.state, 1729))
}

func TestResponsesRejectedMetadataKeepsUnknownInputTypes(test *testing.T) {
	body := []byte(`{"input":[{"type":"future_opaque_state","encrypted_content":"keep","internal_chat_message_metadata_passthrough":{"cell_id":"cell"}}]}`)
	failure := &llm.ResponseError{StatusCode: http.StatusBadRequest, Detail: llm.ErrorDetail{
		Code: "unknown_parameter", Param: "input[0].internal_chat_message_metadata_passthrough.cell_id",
	}}
	_, accepted := responsesRejectedStatusRuleFromError(failure, body)
	require.False(test, accepted)
	require.False(test, hasResponsesInternalMetadata(body))
}

func TestResponsesRejectedReasoningPreservesLiteDeclarations(test *testing.T) {
	definitions := json.RawMessage(`{"type":"additional_tools","tools":[{"type":"function","name":"lookup","async":true,"parameters":{"type":"object","properties":{}}}]}`)
	configuration := json.RawMessage(`{"type":"configuration_update","reasoning":{"effort":"high"}}`)
	items := []json.RawMessage{definitions, configuration}
	for _, item := range gjson.Get(responsesRejectedReasoningFixture, "input").Array() {
		items = append(items, json.RawMessage(item.Raw))
	}
	body, err := sjson.SetBytes([]byte(responsesRejectedReasoningFixture), "input", items)
	require.NoError(test, err)
	outbound := newCodexResponsesPassThroughOutbound()
	outbound.state.RawProviderRequest.Body = body
	middleware := applyResponsesRejectedStatusCompatibility(outbound)
	failure := &llm.ResponseError{StatusCode: http.StatusBadRequest, Detail: llm.ErrorDetail{Code: "invalid_encrypted_content"}}
	middleware.OnOutboundRawError(test.Context(), failure)
	require.True(test, outbound.CanRetry(failure))
	require.NoError(test, outbound.PrepareForRetry(test.Context()))
	request := *outbound.state.RawProviderRequest
	result, err := middleware.OnOutboundRawRequest(test.Context(), &request)
	require.NoError(test, err)
	input := gjson.GetBytes(result.Body, "input").Array()
	require.JSONEq(test, string(definitions), input[0].Raw)
	require.JSONEq(test, string(configuration), input[1].Raw)
	for _, item := range input {
		require.Empty(test, item.Get("encrypted_content").String())
	}
	require.Equal(test, "keep-cache-key", gjson.GetBytes(result.Body, "prompt_cache_key").String())
	require.NotEmpty(test, gjson.GetBytes(body, "input.3.encrypted_content").String())
	middleware.OnOutboundRawError(test.Context(), failure)
	require.False(test, outbound.CanRetry(failure))
}
