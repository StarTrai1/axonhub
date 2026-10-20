package anthropic

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
)

func TestChatToolResponseRetainsExecutableStopReason(t *testing.T) {
	for _, tc := range []struct {
		finish   *string
		expected string
	}{
		{nil, "tool_use"}, {lo.ToPtr("stop"), "tool_use"}, {lo.ToPtr("tool_calls"), "tool_use"},
		{lo.ToPtr("length"), "max_tokens"}, {lo.ToPtr("content_filter"), "refusal"}, {lo.ToPtr("pause_turn"), "pause_turn"},
	} {
		t.Run(tc.expected+"/"+lo.FromPtr(tc.finish), func(t *testing.T) {
			response := convertToAnthropicResponse(&llm.Response{Choices: []llm.Choice{{FinishReason: tc.finish, Message: &llm.Message{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call_exact", Type: "function", Function: llm.FunctionCall{Name: "inspect", Arguments: `{"value":17.125}`}}}}}}})
			require.Equal(t, tc.expected, lo.FromPtr(response.StopReason))
			require.Len(t, response.Content, 1)
			require.Equal(t, "call_exact", response.Content[0].ID)
			require.JSONEq(t, `{"value":17.125}`, string(response.Content[0].Input))
		})
	}
}
