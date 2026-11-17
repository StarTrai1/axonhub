package shared

import (
	"strings"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
)

// IsGPT6Model only matches published model IDs, not unrelated provider aliases.
func IsGPT6Model(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "gpt-6-astra", "gpt-6.1-sol", "gpt-6.1-terra", "gpt-6-sol", "gpt-6-luna":
		return true
	default:
		return IsGPT62Model(model)
	}
}

func IsGPT62Model(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "gpt-6.2-astra", "gpt-6.2-sol":
		return true
	default:
		return false
	}
}

func NormalizeGPT6Effort(model, effort string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if effort == llm.ReasoningEffortMinimal ||
		(effort == llm.ReasoningEffortNone && (model == "gpt-6-astra" || model == "gpt-6.1-sol" || IsGPT62Model(model))) {
		return llm.ReasoningEffortLow
	}
	return effort
}

func GPT6ToolsRequireResponses(model string, request *llm.Request) bool {
	gpt62 := IsGPT62Model(model)
	if request == nil || (!gpt62 && model != "gpt-6.1-sol" && model != "gpt-6.1-terra") {
		return false
	}
	if len(request.Tools) > 0 {
		return true
	}
	for _, message := range request.Messages {
		if len(message.ToolCalls) > 0 || message.Role == "tool" {
			return true
		}
	}
	if !gpt62 || request.ProviderExtensions == nil || request.ProviderExtensions.OpenAIResponses == nil {
		return false
	}
	return responsesExtensionsHaveTools(request.ProviderExtensions.OpenAIResponses.Request)
}

func responsesExtensionsHaveTools(extensions *llm.OpenAIResponsesRequestExtensions) bool {
	if extensions == nil {
		return false
	}
	if len(extensions.RawTools) > 0 {
		return true
	}
	for _, item := range extensions.RawInputItems {
		if isResponsesToolInput(item.Type) {
			return true
		}
	}
	return false
}

func isResponsesToolInput(itemType string) bool {
	switch itemType {
	case "additional_tools", "tool_search_output", "mcp_list_tools", "mcp_approval_request", "mcp_approval_response":
		return true
	default:
		return strings.HasSuffix(itemType, "_call") || strings.HasSuffix(itemType, "_call_output")
	}
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
	if IsGPT62Model(model) && gjson.GetBytes(body, "prompt_cache_retention").Exists() {
		return true
	}
	effort := gjson.GetBytes(body, "reasoning.effort").String()
	if NormalizeGPT6Effort(model, effort) != effort {
		return true
	}
	for _, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("type").String() == "configuration_update" {
			updated := item.Get("reasoning.effort").String()
			if NormalizeGPT6Effort(model, updated) != updated {
				return true
			}
		}
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
