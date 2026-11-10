package openai

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
)

func TestMistralToolHistoryOmitsUnsupportedReasoning(t *testing.T) {
	for _, endpoint := range []string{"https://api.mistral.ai/v1", "https://API.MISTRAL.AI/v1", "https://relay.example/v1", "https://api.mistral.ai.example/v1"} {
		t.Run(endpoint, func(t *testing.T) {
			outbound, err := NewOutboundTransformer(endpoint, "test-key")
			require.NoError(t, err)
			request := &llm.Request{Model: "deepseek-v4", Messages: []llm.Message{
				{Role: "assistant", ReasoningContent: lo.ToPtr("retained reasoning"), ToolCalls: []llm.ToolCall{
					{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "lookup", Arguments: `{}`}},
				}},
				{Role: "tool", ToolCallID: lo.ToPtr("call_1"), Content: llm.MessageContent{Content: lo.ToPtr("result")}},
			}}
			httpRequest, err := outbound.TransformRequest(t.Context(), request)
			require.NoError(t, err)
			wantReasoning := endpoint == "https://relay.example/v1" || endpoint == "https://api.mistral.ai.example/v1"
			require.Equal(t, wantReasoning, gjson.GetBytes(httpRequest.Body, "messages.0.reasoning_content").Exists())
			require.Equal(t, "lookup", gjson.GetBytes(httpRequest.Body, "messages.0.tool_calls.0.function.name").String())
			require.Equal(t, "result", gjson.GetBytes(httpRequest.Body, "messages.1.content").String())
			require.Equal(t, "retained reasoning", *request.Messages[0].ReasoningContent)
		})
	}
}
