package orchestrator

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/objects"
)

func TestRetryPatternCacheKeepsSubstringAndInvalidPatternBehavior(t *testing.T) {
	t.Cleanup(retryPatternCache.Purge)
	rules := []objects.RetryableErrorPattern{{Pattern: `(?i)model.*busy`, Regex: true}}
	err := errors.New("upstream: Model is busy; try later")
	require.True(t, matchesRetryableErrorPattern(err, rules))
	for i := range maxCachedRetryPatterns + 10 {
		require.Nil(t, compiledRetryPattern(fmt.Sprintf("invalid-%d[", i)))
	}
	require.Equal(t, maxCachedRetryPatterns, retryPatternCache.Len())
	require.True(t, matchesRetryableErrorPattern(err, rules))
	require.False(t, matchesRetryableErrorPattern(err, []objects.RetryableErrorPattern{{Pattern: "[", Regex: true}}))
	require.False(t, matchesRetryableErrorPattern(err, []objects.RetryableErrorPattern{{Pattern: "model.*busy", Regex: false}}))
}
