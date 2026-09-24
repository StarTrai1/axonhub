package antigravity

import (
	"fmt"
	"regexp"

	"github.com/looplj/axonhub/llm/transformer/gemini"
)

var invalidClaudeToolIDChars = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// Normalize only the provider-facing Claude IDs. Reserve already-valid IDs
// before assigning aliases so a cleaned ID cannot capture another call's result.
func normalizeClaudeToolIDs(contents []*gemini.Content) {
	used := make(map[string]bool)
	aliases := make(map[string]string)
	for _, content := range contents {
		for _, part := range content.Parts {
			if call := part.FunctionCall; call != nil && call.ID != "" && !invalidClaudeToolIDChars.MatchString(call.ID) {
				used[call.ID] = true
			}
		}
	}
	for _, content := range contents {
		for _, part := range content.Parts {
			call := part.FunctionCall
			if call == nil || (call.ID != "" && !invalidClaudeToolIDChars.MatchString(call.ID)) {
				continue
			}
			rawID := call.ID
			if alias, ok := aliases[rawID]; ok {
				call.ID = alias
				continue
			}
			base := invalidClaudeToolIDChars.ReplaceAllString(rawID, "_")
			if base == "" {
				base = "call"
			}
			alias := base
			for suffix := 1; used[alias]; suffix++ {
				alias = fmt.Sprintf("%s_%d", base, suffix)
			}
			used[alias] = true
			aliases[rawID] = alias
			call.ID = alias
		}
	}
	for _, content := range contents {
		for _, part := range content.Parts {
			if result := part.FunctionResponse; result != nil {
				if alias, ok := aliases[result.ID]; ok {
					result.ID = alias
				}
			}
		}
	}
}
