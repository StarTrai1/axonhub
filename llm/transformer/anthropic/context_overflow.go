package anthropic

import "strings"

// Input overflow is recoverable by compacting; an output limit is a distinct
// normal incomplete result. Keep a provider's explicit code authoritative.
func anthropicContextOverflowCode(code, kind, message string) string {
	if code == "" && kind == "invalid_request_error" && strings.HasPrefix(message, "prompt is too long") {
		return "context_length_exceeded"
	}
	return code
}
