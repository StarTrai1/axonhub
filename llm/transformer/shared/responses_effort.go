package shared

import (
	"strings"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
)

// WithResponsesEffortForConversion resolves ordered effort updates only when
// leaving Responses. Its native top-level effort is part of the cached prefix
// and must remain unchanged when forwarding the original control items.
func WithResponsesEffortForConversion(req *llm.Request) *llm.Request {
	if req == nil || req.RawRequest == nil ||
		(req.APIFormat != llm.APIFormatOpenAIResponse && req.APIFormat != llm.APIFormatOpenAIResponseWebSocket) {
		return req
	}
	body := req.RawRequest.Body
	// Keep an explicit gateway override made after inbound conversion.
	if req.ReasoningEffort != gjson.GetBytes(body, "reasoning.effort").String() {
		return req
	}
	effort := req.ReasoningEffort
	for _, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("type").String() != "configuration_update" {
			continue
		}
		updated := item.Get("reasoning.effort")
		if updated.Type == gjson.String && strings.TrimSpace(updated.Str) != "" {
			effort = updated.Str
		}
	}
	if effort == req.ReasoningEffort {
		return req
	}
	converted := *req
	converted.ReasoningEffort = effort
	return &converted
}
