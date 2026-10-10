package openai

import "github.com/samber/lo"

// adjacentToolResults places each assistant's matching outputs immediately
// after its calls, preserving result order and all intervening messages.
// The input slice is never mutated; other outbound attempts retain native order.
func adjacentToolResults(messages []Message) []Message {
	ordered := make([]Message, 0, len(messages))
	emitted := make([]bool, len(messages))
	for i, message := range messages {
		if emitted[i] {
			continue
		}
		ordered = append(ordered, message)
		if message.Role != "assistant" || len(message.ToolCalls) == 0 {
			continue
		}
		pending := lo.KeyBy(
			lo.Filter(message.ToolCalls, func(call ToolCall, _ int) bool { return call.ID != "" }),
			func(call ToolCall) string { return call.ID },
		)
		for j := i + 1; j < len(messages) && len(pending) > 0; j++ {
			if emitted[j] {
				continue
			}
			candidate := messages[j]
			// A repeated call ID belongs to the newer call occurrence. Do not
			// borrow its output to repair an incomplete earlier occurrence.
			if candidate.Role == "assistant" {
				for _, call := range candidate.ToolCalls {
					delete(pending, call.ID)
				}
			}
			if candidate.Role != "tool" || candidate.ToolCallID == nil {
				continue
			}
			if _, ok := pending[*candidate.ToolCallID]; !ok {
				continue
			}
			ordered = append(ordered, candidate)
			emitted[j] = true
			delete(pending, *candidate.ToolCallID)
		}
	}
	return ordered
}
