package transformer_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestResponsesToolsKeepIdentityAcrossAnthropic(t *testing.T) {
	for _, namespace := range []string{"mcp__" + strings.Repeat("a", 60), "ops.docs"} {
		t.Run(namespace, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"model": "claude-sonnet-5-5",
				"tools": []any{
					map[string]any{"type": "namespace", "name": namespace, "tools": []any{map[string]any{"type": "function", "name": "read", "parameters": map[string]any{"type": "object"}}}},
					map[string]any{"type": "function", "name": "ops/docs__read"},
					map[string]any{"type": "function", "name": "ops_docs__read"},
				},
				"input": []any{
					map[string]any{"type": "function_call", "name": "read", "namespace": namespace, "call_id": "call_1", "arguments": "{}"},
					map[string]any{"type": "function_call_output", "call_id": "call_1", "output": "ok"},
					map[string]any{"role": "user", "content": "continue"},
				},
				"tool_choice": map[string]any{"type": "function", "name": "read", "namespace": namespace},
			})
			require.NoError(t, err)
			inbound := responses.NewInboundTransformer()
			req, err := inbound.TransformRequest(t.Context(), &httpclient.Request{Body: body})
			require.NoError(t, err)
			before, err := json.Marshal(req)
			require.NoError(t, err)
			outbound, err := anthropic.NewOutboundTransformer("https://example.test", "fake-key")
			require.NoError(t, err)
			wire, err := outbound.TransformRequest(t.Context(), req)
			require.NoError(t, err)
			var payload anthropic.MessageRequest
			require.NoError(t, json.Unmarshal(wire.Body, &payload))
			require.Len(t, payload.Tools, 3)
			first := payload.Tools[0].Name
			require.Regexp(t, `^[a-zA-Z0-9_-]{1,64}$`, first)
			require.Regexp(t, `^[a-zA-Z0-9_-]{1,64}$`, payload.Tools[1].Name)
			require.NotEqual(t, first, payload.Tools[1].Name)
			require.Equal(t, "ops_docs__read", payload.Tools[2].Name)
			require.Equal(t, first, *payload.ToolChoice.Name)
			found := false
			for _, message := range payload.Messages {
				for _, block := range message.Content.MultipleContent {
					if block.Type == "tool_use" {
						require.Equal(t, "call_1", block.ID)
						require.Equal(t, first, *block.Name)
						found = true
					}
				}
			}
			require.True(t, found)
			chat := openai.RequestFromLLM(t.Context(), req, openai.ReasoningFieldContent)
			require.Equal(t, first, chat.Tools[0].Function.Name)
			after, err := json.Marshal(req)
			require.NoError(t, err)
			require.Equal(t, before, after)

			// Persisted metadata restores the original name and namespace on return.
			encoded, err := json.Marshal(req.TransformerMetadata)
			require.NoError(t, err)
			var metadata map[string]any
			require.NoError(t, json.Unmarshal(encoded, &metadata))
			result, err := inbound.TransformResponse(t.Context(), &llm.Response{
				TransformerMetadata: metadata,
				Choices: []llm.Choice{{Message: &llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{
					ID: "call_2", Type: "function", Function: llm.FunctionCall{Name: first, Arguments: "{}"},
				}}}}},
			})
			require.NoError(t, err)
			var native responses.Response
			require.NoError(t, json.Unmarshal(result.Body, &native))
			require.Equal(t, "read", native.Output[0].Name)
			require.Equal(t, namespace, native.Output[0].Namespace)
		})
	}
}
