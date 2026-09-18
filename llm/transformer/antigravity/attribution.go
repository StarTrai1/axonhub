package antigravity

import "strings"

// stripClaudeAttribution removes only a leading Claude Code billing metadata
// line. Later quoted examples and the instructions following that line survive.
func stripClaudeAttribution(text string) string {
	trimmed := strings.TrimLeft(text, " \t\r\n")
	if !strings.HasPrefix(trimmed, "x-anthropic-billing-header:") {
		return text
	}
	end := strings.IndexAny(trimmed, "\r\n")
	if end < 0 {
		return ""
	}
	rest := trimmed[end+1:]
	if trimmed[end] == '\r' {
		rest = strings.TrimPrefix(rest, "\n")
	}
	return rest
}
