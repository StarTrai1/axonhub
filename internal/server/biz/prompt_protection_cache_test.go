package biz

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPromptProtectionPatternCacheBoundsAndEviction(t *testing.T) {
	promptProtectionRegexCache.Purge()
	t.Cleanup(promptProtectionRegexCache.Purge)
	require.True(t, MatchPromptProtectionRule(`secret-\d+`, "secret-123"))
	for i := range maxPromptProtectionPatterns + 10 {
		_, err := getOrCompilePromptProtectionPattern(fmt.Sprintf("invalid-%d[", i))
		require.Error(t, err)
	}
	require.Equal(t, maxPromptProtectionPatterns, promptProtectionRegexCache.Len())
	require.True(t, MatchPromptProtectionRule(`secret-\d+`, "secret-123"))
	require.Equal(t, "redacted", ReplacePromptProtectionRule(`secret-\d+`, "secret-123", "redacted"))
	_, err := getOrCompilePromptProtectionPattern("invalid-0[")
	require.Error(t, err)
}
