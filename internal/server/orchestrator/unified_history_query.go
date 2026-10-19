package orchestrator

import (
	"encoding/json"
	"strings"
)

// Codex question replies include transport IDs and tool names. These are not
// user search terms: matching them recalls unrelated tool documentation and
// rewrites an otherwise reusable history prefix.
func unifiedHistoryQuery(item json.RawMessage) string {
	text := strings.TrimSpace(unifiedMessageText(item))
	const open = "<send_user_message_question_reply>"
	const close = "</send_user_message_question_reply>"
	if strings.HasPrefix(text, open) && strings.HasSuffix(text, close) {
		payload := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, open), close))
		var replies []struct {
			Question string `json:"question"`
			Answer string `json:"answer"`
		}
		if json.Unmarshal([]byte(payload), &replies) == nil && len(replies) > 0 {
			var parts []string
			for _, reply := range replies {
				parts = append(parts, reply.Question, reply.Answer)
			}
			text = strings.Join(parts, "\n")
		}
	}
	return strings.ToLower(text)
}
