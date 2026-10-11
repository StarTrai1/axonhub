package gemini

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
)

func TestGemini25ThinkingLimits(t *testing.T) {
	for _, tc := range []struct {
		model  string
		effort string
		budget int64
	}{
		{"gemini-2.5-pro", "high", 24576},
		{"gemini-2.5-pro", "xhigh", 32768},
		{"models/gemini-2.5-pro-preview-06-05", "max", 32768},
		{"gemini-2.5-pro", "none", 128},
		{"gemini-2.5-flash", "max", 24576},
		{"gemini-2.5-flash-lite", "xhigh", 24576},
		{"gemini-2.5-flash", "none", 0},
		{"gemini-2.5-flash", "minimal", 1024},
	} {
		t.Run(tc.model+"/"+tc.effort, func(t *testing.T) {
			req := convertLLMToGeminiRequest(&llm.Request{Model: tc.model, ReasoningEffort: tc.effort})
			require.Equal(t, tc.budget, *req.GenerationConfig.ThinkingConfig.ThinkingBudget)
			require.Empty(t, req.GenerationConfig.ThinkingConfig.ThinkingLevel)
		})
	}

	for _, budget := range []int64{-1, 8192, 32768, 50000} {
		req := convertLLMToGeminiRequest(&llm.Request{Model: "gemini-2.5-pro", ReasoningBudget: lo.ToPtr(budget)})
		require.Equal(t, min(budget, int64(32768)), *req.GenerationConfig.ThinkingConfig.ThinkingBudget)
	}
	config := &Config{ReasoningEffortToBudget: map[string]int64{"high": 16000}}
	req := convertLLMToGeminiRequestWithConfig(&llm.Request{Model: "gemini-2.5-pro", ReasoningEffort: "high"}, config)
	ensure := req.GenerationConfig.ThinkingConfig
	require.Equal(t, int64(16000), *ensure.ThinkingBudget)
}

func TestGeminiThinkingLimitsPreserveNativeConfigAndGemini3Levels(t *testing.T) {
	req := convertLLMToGeminiRequest(&llm.Request{
		Model: "gemini-2.5-pro", ReasoningEffort: "max",
		ExtraBody: json.RawMessage(`{"google":{"thinking_config":{"thinking_budget":50000,"include_thoughts":false}}}`),
	})
	require.Equal(t, int64(50000), *req.GenerationConfig.ThinkingConfig.ThinkingBudget)
	require.False(t, req.GenerationConfig.ThinkingConfig.IncludeThoughts)
	for _, effort := range []string{"xhigh", "max"} {
		req = convertLLMToGeminiRequest(&llm.Request{Model: "gemini-3-pro-preview", ReasoningEffort: effort})
		require.Equal(t, "high", req.GenerationConfig.ThinkingConfig.ThinkingLevel)
		require.Nil(t, req.GenerationConfig.ThinkingConfig.ThinkingBudget)
	}
}
