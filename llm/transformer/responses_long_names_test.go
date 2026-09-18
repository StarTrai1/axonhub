package transformer_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestResponsesLongNamesUseConsistentChatAliases(t *testing.T) {
	namespace := "mcp__documents__" + strings.Repeat("a", 52)
	other := "mcp__other__" + strings.Repeat("a", 52)
	input := map[string]any{
		"model": "test",
		"tools": []any{
			map[string]any{"type": "namespace", "name": namespace, "tools": []any{map[string]any{"type": "function", "name": "search"}}},
			map[string]any{"type": "namespace", "name": other, "tools": []any{map[string]any{"type": "function", "name": "search"}}},
			map[string]any{"type": "function", "name": "plain"},
		},
		"input": []any{
			map[string]any{"type": "function_call", "name": "search", "namespace": namespace, "call_id": "call_1", "arguments": "{}"},
			map[string]any{"type": "function_call_output", "call_id": "call_1", "output": "ok"},
		},
		"tool_choice": map[string]any{"type": "function", "name": "search", "namespace": namespace},
	}
	body, err := json.Marshal(input)
	require.NoError(t, err)
	inbound := responses.NewInboundTransformer()
	req, err := inbound.TransformRequest(t.Context(), &httpclient.Request{Body: body})
	require.NoError(t, err)
	before, err := json.Marshal(req)
	require.NoError(t, err)
	chat := openai.RequestFromLLM(t.Context(), req, openai.ReasoningFieldContent)
	first := chat.Tools[0].Function.Name
	require.LessOrEqual(t, len(first), 64)
	require.LessOrEqual(t, len(chat.Tools[1].Function.Name), 64)
	require.NotEqual(t, first, chat.Tools[1].Function.Name)
	require.Equal(t, "plain", chat.Tools[2].Function.Name)
	require.Equal(t, first, chat.Messages[0].ToolCalls[0].Function.Name)
	require.Equal(t, first, *chat.Messages[1].Name)
	require.Equal(t, first, chat.ToolChoice.NamedToolChoice.Function.Name)
	require.Equal(t, "call_1", chat.Messages[0].ToolCalls[0].ID)
	require.Equal(t, "call_1", *chat.Messages[1].ToolCallID)
	after, err := json.Marshal(req)
	require.NoError(t, err)
	require.Equal(t, before, after)

	// Reserve a short tool that deliberately occupies the first generated alias.
	input["tools"] = append(input["tools"].([]any), map[string]any{"type": "function", "name": first})
	body, err = json.Marshal(input)
	require.NoError(t, err)
	req, err = inbound.TransformRequest(t.Context(), &httpclient.Request{Body: body})
	require.NoError(t, err)
	chat = openai.RequestFromLLM(t.Context(), req, openai.ReasoningFieldContent)
	require.Equal(t, first, chat.Tools[3].Function.Name)
	require.NotEqual(t, first, chat.Tools[0].Function.Name)
	require.LessOrEqual(t, len(chat.Tools[0].Function.Name), 64)

	// Metadata survives persistence/JSON round trips and restores the native identity.
	encoded, err := json.Marshal(req.TransformerMetadata)
	require.NoError(t, err)
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(encoded, &metadata))
	result, err := inbound.TransformResponse(t.Context(), &llm.Response{
		TransformerMetadata: metadata,
		Choices: []llm.Choice{{Message: &llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID: "call_2", Type: "function", Function: llm.FunctionCall{Name: chat.Tools[0].Function.Name, Arguments: "{}"},
		}}}}},
	})
	require.NoError(t, err)
	var native responses.Response
	require.NoError(t, json.Unmarshal(result.Body, &native))
	require.Equal(t, "search", native.Output[0].Name)
	require.Equal(t, namespace, native.Output[0].Namespace)

	outbound, err := responses.NewOutboundTransformer("https://example.com", "test")
	require.NoError(t, err)
	wire, err := outbound.TransformRequest(t.Context(), req)
	require.NoError(t, err)
	var nativeRequest responses.Request
	require.NoError(t, json.Unmarshal(wire.Body, &nativeRequest))
	require.Equal(t, namespace, nativeRequest.Tools[0].Name)
	require.Equal(t, "search", nativeRequest.Tools[0].Tools[0].Name)
}
