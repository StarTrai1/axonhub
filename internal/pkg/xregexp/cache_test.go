package xregexp

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPatternCacheBoundsAndEviction(t *testing.T) {
	globalCache.Purge()
	t.Cleanup(globalCache.Purge)
	for i := range maxCachedPatterns + 10 {
		literal := fmt.Sprintf("model-%d", i)
		require.True(t, MatchString(literal, literal))
	}
	require.Zero(t, globalCache.Len(), "literal model names must not accumulate in the compiled cache")
	require.True(t, MatchString("(?i)gpt-[0-9]+", "GPT-6"))
	for i := range maxCachedPatterns + 10 {
		require.Error(t, ValidateRegex(fmt.Sprintf("broken-%d[", i)))
	}
	require.Equal(t, maxCachedPatterns, globalCache.Len())
	// Eviction changes storage, not full-match or invalid-pattern behavior.
	require.True(t, MatchString("(?i)gpt-[0-9]+", "GPT-6"))
	require.False(t, MatchString("(?i)gpt-[0-9]+", "prefix-GPT-6"))
	require.Error(t, ValidateRegex("broken-0["))
}
