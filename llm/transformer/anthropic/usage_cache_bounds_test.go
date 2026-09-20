package anthropic

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
)

func TestAnthropicInputUsageSubtractsCachesWithoutOverflow(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		input, read, write, expected int64
	}{
		{name: "both caches", input: 100, read: 30, write: 20, expected: 50},
		{name: "cache exceeds total", input: 10, read: 8, write: 8},
		{name: "cache sum overflows", input: math.MaxInt64, read: math.MaxInt64, write: math.MaxInt64},
		{name: "negative input", input: -1},
		{name: "negative cache does not inflate input", input: 100, read: -30, write: 20, expected: 80},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &llm.Usage{PromptTokens: tc.input, PromptTokensDetails: &llm.PromptTokensDetails{CachedTokens: tc.read, WriteCachedTokens: tc.write}}
			got := convertToAnthropicUsage(source)
			require.Equal(t, tc.expected, got.InputTokens)
			require.Equal(t, tc.read, got.CacheReadInputTokens)
			require.Equal(t, tc.write, got.CacheCreationInputTokens)
			require.Equal(t, tc.input, source.PromptTokens)
		})
	}
}
