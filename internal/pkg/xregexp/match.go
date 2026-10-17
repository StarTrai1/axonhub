package xregexp

import (
	"fmt"
	"strings"

	"github.com/dlclark/regexp2/v2"
	"github.com/samber/lo"

	lru "github.com/hashicorp/golang-lru/v2"
)

type patternCache struct {
	regex      *regexp2.Regexp
	exactMatch bool
	compileErr bool
	matchAll   bool
}

const maxCachedPatterns = 1024

var (
	globalCache  = lo.Must(lru.New[string, *patternCache](maxCachedPatterns))
	exactPattern = &patternCache{exactMatch: true}
	allPattern   = &patternCache{matchAll: true}
)

func MatchString(pattern string, str string) bool {
	cached := getOrCreatePattern(pattern)

	if cached.compileErr {
		return false
	}

	if cached.exactMatch {
		return pattern == str
	}

	if cached.matchAll {
		return true
	}

	match, _ := cached.regex.MatchString(str)

	return match
}

func Filter(items []string, pattern string) []string {
	if pattern == "" {
		return []string{}
	}

	cached := getOrCreatePattern(pattern)

	if cached.compileErr {
		return []string{}
	}

	matched := make([]string, 0)

	if cached.exactMatch {
		for _, item := range items {
			if pattern == item {
				matched = append(matched, item)
			}
		}
	} else if cached.matchAll {
		return append(matched, items...)
	} else {
		for _, item := range items {
			if match, _ := cached.regex.MatchString(item); match {
				matched = append(matched, item)
			}
		}
	}

	return matched
}

func getOrCreatePattern(pattern string) *patternCache {
	if pattern == "*" {
		return allPattern
	}

	if !containsRegexChars(pattern) {
		return exactPattern
	}
	if cached, ok := globalCache.Get(pattern); ok {
		return cached
	}

	cached := &patternCache{}
	compiled, err := regexp2.Compile(ensureAnchored(pattern), regexp2.None)
	if err != nil {
		cached.compileErr = true
	} else {
		cached.regex = compiled
	}

	globalCache.Add(pattern, cached)

	return cached
}

func ensureAnchored(pattern string) string {
	modifier, body := splitInlineModifier(pattern)
	body = strings.TrimPrefix(body, "^")
	body = strings.TrimSuffix(body, "$")

	return modifier + "^(?:" + body + ")$"
}

func ValidateRegex(pattern string) error {
	if pattern == "" {
		return nil
	}

	cached := getOrCreatePattern(pattern)
	if cached.compileErr {
		return fmt.Errorf("invalid regex pattern: %s", pattern)
	}

	return nil
}

func containsRegexChars(pattern string) bool {
	return strings.ContainsAny(pattern, "*?+[]{}()^$.|\\")
}

func splitInlineModifier(pattern string) (string, string) {
	if !strings.HasPrefix(pattern, "(?") {
		return "", pattern
	}

	end := strings.Index(pattern, ")")
	if end <= 2 {
		return "", pattern
	}

	modifier := pattern[:end+1]
	body := pattern[end+1:]

	if strings.ContainsAny(modifier[2:end], ":=!<") {
		return "", pattern
	}

	return modifier, body
}
