package orchestrator

import (
	"regexp"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/samber/lo"
)

const maxCachedRetryPatterns = 1024

// Keep standard-library substring matching: model selectors use a different,
// anchored regex dialect. Cache failures too, with a bound across config edits.
var retryPatternCache = lo.Must(lru.New[string, *regexp.Regexp](maxCachedRetryPatterns))

func compiledRetryPattern(pattern string) *regexp.Regexp {
	if compiled, ok := retryPatternCache.Get(pattern); ok {
		return compiled
	}
	compiled, _ := regexp.Compile(pattern)
	retryPatternCache.Add(pattern, compiled)
	return compiled
}
