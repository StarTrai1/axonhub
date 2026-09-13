package anthropic

import "github.com/tidwall/gjson"

// Anthropic message_delta usage fields are cumulative counters. Apply only
// fields reported by this event; absent/null counters retain their earlier
// values, while an explicit zero replaces them. Merge before converting to
// unified totals so a cache-only update does not lose uncached input tokens.
func mergeAnthropicUsage(previous, delta *Usage, fields gjson.Result) *Usage {
	if delta == nil {
		return previous
	}
	if previous == nil {
		result := *delta
		return &result
	}
	result := *previous
	present := func(path string) bool {
		value := fields.Get(path)
		return value.Exists() && value.Type != gjson.Null
	}
	if present("input_tokens") {
		result.InputTokens = delta.InputTokens
	}
	if present("output_tokens") {
		result.OutputTokens = delta.OutputTokens
	}
	if present("cache_creation_input_tokens") {
		result.CacheCreationInputTokens = delta.CacheCreationInputTokens
	}
	if present("cache_read_input_tokens") {
		result.CacheReadInputTokens = delta.CacheReadInputTokens
	}
	if present("cached_tokens") {
		result.CachedTokens = delta.CachedTokens
	}
	if present("cache_creation.ephemeral_5m_input_tokens") {
		result.CacheCreation.Ephemeral5mInputTokens = delta.CacheCreation.Ephemeral5mInputTokens
	}
	if present("cache_creation.ephemeral_1h_input_tokens") {
		result.CacheCreation.Ephemeral1hInputTokens = delta.CacheCreation.Ephemeral1hInputTokens
	}
	if present("output_tokens_details") {
		result.OutputTokensDetails = delta.OutputTokensDetails
	}
	if present("service_tier") {
		result.ServiceTier = delta.ServiceTier
	}
	return &result
}
