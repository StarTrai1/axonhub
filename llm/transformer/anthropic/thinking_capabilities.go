package anthropic

import (
	"strings"

	"github.com/looplj/axonhub/llm"
)

// Match explicit model families and dated aliases without assigning capabilities
// to unknown future versions. Bedrock and Vertex may qualify the model name.
func claudeModelMatches(model, name string) bool {
	model = strings.ToLower(strings.TrimSpace(model))
	if index := strings.LastIndex(model, "claude-"); index >= 0 {
		model = model[index:]
	}
	return model == name || model == name+"-latest" ||
		strings.HasPrefix(model, name+"-20") || strings.HasPrefix(model, name+"@20")
}

// These rules apply only to translated OpenAI requests. Native Anthropic
// parameters and third-party Anthropic-compatible providers keep their contract.
// https://platform.claude.com/docs/en/build-with-claude/effort
// https://platform.claude.com/docs/en/build-with-claude/thinking-troubleshooting
func normalizeConvertedClaudeThinking(src *llm.Request, dst *MessageRequest, config *Config) {
	if !shouldDecodeAnthropicSignature(config) {
		return
	}
	if src.APIFormat != llm.APIFormatOpenAIChatCompletion &&
		src.APIFormat != llm.APIFormatOpenAIResponse && src.APIFormat != llm.APIFormatOpenAIResponseWebSocket {
		return
	}

	is := func(name string) bool { return claudeModelMatches(src.Model, name) }
	alwaysThinking := is("claude-opus-5-5") || is("claude-fable-5") || is("claude-fable-5-1") ||
		is("claude-mythos-5") || is("claude-mythos-5-1") || is("claude-mythos-preview")
	extendedOnly := is("claude-opus-4-5") || is("claude-sonnet-4-5") || is("claude-haiku-4-5")
	adaptiveOnly := (alwaysThinking && !is("claude-mythos-preview")) || is("claude-opus-5") || is("claude-opus-4-8") ||
		is("claude-opus-4-7") || is("claude-sonnet-5") || is("claude-sonnet-5-5") || is("claude-haiku-5-5")
	legacyAdaptive := is("claude-opus-4-6") || is("claude-sonnet-4-6")
	if !extendedOnly && !adaptiveOnly && !legacyAdaptive && !is("claude-mythos-preview") {
		return
	}

	if dst.OutputConfig != nil {
		effort := dst.OutputConfig.Effort
		switch {
		case is("claude-sonnet-4-5"), is("claude-haiku-4-5"):
			dst.OutputConfig = nil
		case is("claude-opus-4-5") && (effort == "xhigh" || effort == "max"):
			dst.OutputConfig.Effort = "high"
		case (legacyAdaptive || is("claude-mythos-preview")) && effort == "xhigh":
			dst.OutputConfig.Effort = "max"
		}
	}
	if dst.Thinking != nil {
		switch {
		case dst.Thinking.Type == "disabled" && alwaysThinking:
			dst.Thinking = &Thinking{Type: "adaptive"}
			dst.OutputConfig = &OutputConfig{Effort: "low"}
		case dst.Thinking.Type == "disabled" && is("claude-sonnet-5-5"):
			dst.Thinking = &Thinking{Type: "between_tools"}
			if dst.OutputConfig == nil {
				dst.OutputConfig = &OutputConfig{Effort: "low"}
			}
		case dst.Thinking.Type == "enabled" && adaptiveOnly:
			budget := dst.Thinking.BudgetTokens
			dst.Thinking = &Thinking{Type: "adaptive"}
			if dst.OutputConfig == nil {
				dst.OutputConfig = &OutputConfig{Effort: thinkingBudgetToReasoningEffort(budget)}
			}
		case dst.Thinking.Type == "adaptive" && extendedOnly:
			dst.Thinking = buildThinking(src, config)
			if dst.MaxTokens <= 1024 {
				dst.Thinking = &Thinking{Type: "disabled"}
			} else {
				dst.Thinking.BudgetTokens = min(max(dst.Thinking.BudgetTokens, 1024), dst.MaxTokens-1)
			}
		}
		if dst.OutputConfig != nil && (dst.Thinking.Type == "disabled" || dst.Thinking.Type == "between_tools") &&
			(is("claude-opus-5") || is("claude-haiku-5-5") || is("claude-sonnet-5-5")) &&
			(dst.OutputConfig.Effort == "xhigh" || dst.OutputConfig.Effort == "max") {
			dst.OutputConfig.Effort = "high"
		}
	}
	if adaptiveOnly || (dst.Thinking != nil && dst.Thinking.Type != "disabled") {
		dst.Temperature = nil
		dst.TopP = nil
	}
}
