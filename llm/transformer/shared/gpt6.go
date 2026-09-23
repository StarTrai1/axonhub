package shared

import (
	"strings"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
)

// IsGPT6Model only matches published model IDs, not unrelated provider aliases.
func IsGPT6Model(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "gpt-6-astra", "gpt-6-sol", "gpt-6-luna":
		return true
	default:
		return false
	}
}

func NormalizeGPT6Effort(model, effort string) string {
	if effort == llm.ReasoningEffortMinimal ||
		(effort == llm.ReasoningEffortNone && strings.EqualFold(strings.TrimSpace(model), "gpt-6-astra")) {
		return llm.ReasoningEffortLow
	}
	return effort
}

// GPT6EffectiveEffort follows ordered configuration updates without changing the
// request-level effort, which remains part of the reusable prompt prefix.
func GPT6EffectiveEffort(model, effort string, body []byte) string {
	for _, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("type").String() == "configuration_update" {
			if updated := item.Get("reasoning.effort").String(); updated != "" {
				effort = updated
			}
		}
	}
	return NormalizeGPT6Effort(model, effort)
}

func GPT6ResponsesNeedsNormalization(model string, body []byte) bool {
	if !IsGPT6Model(model) {
		return false
	}
	effort := gjson.GetBytes(body, "reasoning.effort").String()
	if NormalizeGPT6Effort(model, effort) != effort {
		return true
	}
	if GPT6EffectiveEffort(model, effort, body) == llm.ReasoningEffortNone {
		return false
	}
	for _, field := range []string{"temperature", "top_p", "top_logprobs"} {
		if gjson.GetBytes(body, field).Exists() {
			return true
		}
	}
	for _, field := range gjson.GetBytes(body, "include").Array() {
		if field.String() == "message.output_text.logprobs" {
			return true
		}
	}
	return false
}
