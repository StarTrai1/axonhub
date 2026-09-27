package antigravity

import (
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm"
)

// Keep summary visibility separate from the thinking budget/level. Patch the
// wire body so an explicit false survives ThinkingConfig's omitempty field.
func applyResponsesSummaryVisibility(body []byte, request *llm.Request) ([]byte, error) {
	if request.APIFormat != llm.APIFormatOpenAIResponse {
		return body, nil
	}
	summary := ""
	if request.ReasoningSummary != nil {
		summary = *request.ReasoningSummary
	}
	if request.RawRequest != nil {
		value := gjson.GetBytes(request.RawRequest.Body, "reasoning.summary")
		if !value.Exists() {
			value = gjson.GetBytes(request.RawRequest.Body, "reasoning.generate_summary")
		}
		// Canonical summary, including null, takes precedence over the alias.
		if value.Exists() {
			if value.Type == gjson.Null {
				summary = "none"
			} else if value.Type == gjson.String {
				summary = value.String()
			} else {
				return body, nil
			}
		}
	}
	var enabled bool
	switch strings.ToLower(strings.TrimSpace(summary)) {
	case "none":
		enabled = false
	case "auto", "concise", "detailed":
		enabled = true
	default:
		return body, nil
	}
	return sjson.SetBytes(body, "request.generationConfig.thinkingConfig.includeThoughts", enabled)
}
