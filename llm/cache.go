package llm

// CacheControl represents cache control configuration.
// This field is used internally for provider-specific cache control
// and should not be serialized in the standard llm JSON format.
type CacheControl struct {
	Type string `json:"type,omitempty"`
	TTL  string `json:"ttl,omitempty"`
}

// PromptCacheOptions controls OpenAI GPT-5.6+ prompt-cache breakpoint behavior.
// It is kept separate from Anthropic CacheControl because the protocols use
// different semantics and wire shapes.
type PromptCacheOptions struct {
	Mode string `json:"mode,omitempty"`
	TTL  string `json:"ttl,omitempty"`
}

// PromptCacheBreakpoint marks the end of a reusable OpenAI prompt prefix.
type PromptCacheBreakpoint struct {
	Mode string `json:"mode,omitempty"`
}
