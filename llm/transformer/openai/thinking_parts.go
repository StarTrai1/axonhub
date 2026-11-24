package openai

import (
	"encoding/json"
	"strings"

	"github.com/samber/lo"
)

// Chat-compatible providers may alternate string text and structured thinking
// parts in message/delta.content. Normalize both into the shared reasoning fields.
func normalizeChatThinkingParts(message Message) Message {
	if len(message.Content.MultipleContent) == 0 || (message.Role != "" && message.Role != "assistant") {
		return message
	}
	var reasoning strings.Builder
	parts := make([]MessageContentPart, 0, len(message.Content.MultipleContent))
	found := false
	for _, part := range message.Content.MultipleContent {
		if part.Type != "thinking" {
			parts = append(parts, part)
			continue
		}
		found = true
		var text string
		if json.Unmarshal(part.Thinking, &text) == nil {
			reasoning.WriteString(text)
			continue
		}
		var chunks []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(part.Thinking, &chunks) == nil {
			for _, chunk := range chunks {
				if chunk.Type == "text" {
					reasoning.WriteString(chunk.Text)
				}
			}
		}
	}
	if found {
		message.Content.MultipleContent = parts
		if lo.FromPtr(message.ReasoningContent) == "" && lo.FromPtr(message.Reasoning) == "" && lo.FromPtr(message.ReasoningText) == "" && reasoning.Len() > 0 {
			message.ReasoningContent = lo.ToPtr(reasoning.String())
		}
	}
	return message
}
