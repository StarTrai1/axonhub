package openai

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestChatThinkingPartsPreserveTextAndReasoning(t *testing.T) {
	for _, field := range []string{"message", "delta"} {
		var response Response
		require.NoError(t, json.Unmarshal([]byte(`{"choices":[{"`+field+`":{"role":"assistant","content":[{"type":"thinking","thinking":[{"type":"text","text":"We "},{"type":"text","text":"add"}]},{"type":"text","text":"391"},{"type":"thinking","thinking":"."}]}}]}`), &response))
		converted := response.ToLLMResponse().Choices[0]
		message := converted.Message
		if field == "delta" {
			message = converted.Delta
		}
		require.Equal(t, "We add.", lo.FromPtr(message.ReasoningContent))
		require.Equal(t, "We add.", lo.FromPtr(message.Reasoning))
		require.Len(t, message.Content.MultipleContent, 1)
		require.Equal(t, "391", lo.FromPtr(message.Content.MultipleContent[0].Text))
		// The original parsed value remains unchanged for native replay.
		original := response.Choices[0].Message
		if field == "delta" {
			original = response.Choices[0].Delta
		}
		require.Len(t, original.Content.MultipleContent, 3)
	}
	var message Message
	require.NoError(t, json.Unmarshal([]byte(`{"role":"assistant","reasoning_content":"explicit","content":[{"type":"thinking","thinking":"duplicate"},{"type":"text","text":"answer"}]}`), &message))
	require.Equal(t, "explicit", lo.FromPtr(message.ToLLMMessage().ReasoningContent))
	require.NoError(t, json.Unmarshal([]byte(`{"role":"assistant","content":"next text"}`), &message))
	require.Equal(t, "next text", lo.FromPtr(message.ToLLMMessage().Content.Content))
}
