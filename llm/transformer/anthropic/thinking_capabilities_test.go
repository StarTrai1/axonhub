package anthropic

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
)

func TestConvertedClaudeThinkingCapabilities(t *testing.T) {
	for _, tc := range []struct {
		model    string
		effort   string
		thinking string
		output   string
	}{
		{"claude-opus-4-5-20251101", "max", "enabled", "high"},
		{"claude-sonnet-4-5", "xhigh", "enabled", ""},
		{"claude-haiku-4-5", "high", "enabled", ""},
		{"claude-opus-4-6", "xhigh", "adaptive", "max"},
		{"claude-mythos-preview", "xhigh", "adaptive", "max"},
		{"claude-fable-5", "none", "adaptive", "low"},
		{"claude-opus-5-5", "none", "adaptive", "low"},
		{"claude-sonnet-5-5", "none", "between_tools", "low"},
		{"claude-haiku-5-5", "max", "adaptive", "max"},
		{"claude-haiku-5-5", "none", "disabled", ""},
	} {
		t.Run(tc.model+"/"+tc.effort, func(t *testing.T) {
			src := &llm.Request{
				APIFormat: llm.APIFormatOpenAIResponse, Model: tc.model, ReasoningEffort: tc.effort,
				MaxTokens: lo.ToPtr(int64(8192)), Temperature: lo.ToPtr(0.7), TopP: lo.ToPtr(0.9),
			}
			dst := convertToAnthropicRequestWithConfig(src, &Config{Type: PlatformDirect})
			require.Equal(t, tc.thinking, dst.Thinking.Type)
			if tc.output == "" {
				require.Nil(t, dst.OutputConfig)
			} else {
				require.Equal(t, tc.output, dst.OutputConfig.Effort)
			}
			if tc.thinking == "enabled" {
				require.Equal(t, int64(8191), dst.Thinking.BudgetTokens)
			}
			require.Nil(t, dst.Temperature)
			require.Nil(t, dst.TopP)
			require.Equal(t, 0.7, *src.Temperature)
			require.Equal(t, tc.effort, src.ReasoningEffort)
		})
	}
}

func TestConvertedClaudeThinkingKeepsNativeAndUnknownContracts(t *testing.T) {
	src := &llm.Request{
		Model: "claude-opus-4-5", ReasoningEffort: "max", Temperature: lo.ToPtr(0.7),
		TransformerMetadata: map[string]any{
			TransformerMetadataKeyThinkingType: "adaptive", TransformerMetadataKeyOutputConfigEffort: "max",
		},
	}
	dst := convertToAnthropicRequestWithConfig(src, &Config{Type: PlatformDirect})
	require.Equal(t, "adaptive", dst.Thinking.Type)
	require.Equal(t, "max", dst.OutputConfig.Effort)
	require.Equal(t, 0.7, *dst.Temperature)

	src.TransformerMetadata = nil
	src.APIFormat = llm.APIFormatOpenAIChatCompletion
	src.Model = "claude-opus-5-6"
	dst = convertToAnthropicRequestWithConfig(src, &Config{Type: PlatformDirect})
	require.Equal(t, "max", dst.OutputConfig.Effort)
	require.NotNil(t, dst.Temperature)

	src.Model = "claude-opus-4-5"
	dst = convertToAnthropicRequestWithConfig(src, &Config{Type: PlatformDeepSeek})
	require.Equal(t, "max", dst.OutputConfig.Effort)
	require.NotNil(t, dst.Temperature)
}

func TestConvertedClaudeBudgetAndSourceDefaults(t *testing.T) {
	src := &llm.Request{
		APIFormat: llm.APIFormatOpenAIChatCompletion, Model: "claude-haiku-5-5", ReasoningBudget: lo.ToPtr(int64(40000)),
	}
	dst := convertToAnthropicRequestWithConfig(src, &Config{Type: PlatformDirect})
	require.Equal(t, "adaptive", dst.Thinking.Type)
	require.Zero(t, dst.Thinking.BudgetTokens)
	require.Equal(t, "xhigh", dst.OutputConfig.Effort)
	require.Equal(t, int64(40000), *src.ReasoningBudget)
	for _, model := range []string{"claude-haiku-5-5", "claude-haiku-5-5-20261001", "claude-opus-5-5"} {
		require.Equal(t, "medium", defaultAnthropicReasoningEffort(model))
	}
	require.Equal(t, "high", defaultAnthropicReasoningEffort("claude-haiku-5-6"))
}
