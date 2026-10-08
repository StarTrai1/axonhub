package openai

import (
	"fmt"

	"github.com/samber/lo"
)

// Normalize deprecated function history before converting to the shared tool
// representation. IDs are deterministic and never collide with modern calls.
func normalizeLegacyFunctionMessages(messages []Message) error {
	used := make(map[string]bool)
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			used[call.ID] = true
		}
		if message.ToolCallID != nil {
			used[*message.ToolCallID] = true
		}
	}
	pending := make(map[string][]string)
	next := 0
	for i := range messages {
		message := &messages[i]
		if call := message.FunctionCall; call != nil {
			if message.Role != "assistant" || call.Name == "" || len(message.ToolCalls) > 0 {
				return fmt.Errorf("messages[%d]: invalid or ambiguous legacy function_call", i)
			}
			id := ""
			for {
				id = fmt.Sprintf("call_legacy_%d", next)
				next++
				if !used[id] {
					break
				}
			}
			used[id] = true
			pending[call.Name] = append(pending[call.Name], id)
			message.ToolCalls = []ToolCall{{ID: id, Type: "function", Function: *call}}
			message.FunctionCall = nil
		}
		if message.Role == "function" {
			name := lo.FromPtr(message.Name)
			ids := pending[name]
			if len(ids) == 0 {
				return fmt.Errorf("messages[%d]: legacy function result has no matching call", i)
			}
			message.Role = "tool"
			message.ToolCallID = lo.ToPtr(ids[0])
			pending[name] = ids[1:]
		}
	}
	return nil
}
