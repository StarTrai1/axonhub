package responses

import (
	"encoding/json"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/openai"
)

type Usage struct {
	InputTokens       int64 `json:"input_tokens"`
	InputTokenDetails struct {
		// CacheWriteTokens is the number of input tokens written to the prompt cache.
		CacheWriteTokens int64 `json:"cache_write_tokens"`
		// CachedTokens is the number of input tokens retrieved from the prompt cache.
		CachedTokens int64 `json:"cached_tokens"`

		// CachedTokensDetails contains optional subsets, not additional usage.
		CachedTokensDetails *llm.CachedTokensDetails `json:"cached_tokens_details,omitempty"`
	} `json:"input_tokens_details"`
	OutputTokens       int64 `json:"output_tokens"`
	OutputTokenDetails struct {
		ReasoningTokens int64 `json:"reasoning_tokens"`
	} `json:"output_tokens_details"`
	TotalTokens int64    `json:"total_tokens"`
	Cost        *float64 `json:"cost,omitempty"`
}

// UnmarshalJSON accepts the same cache-write aliases as Chat Completions while
// keeping the Responses wire fields and inclusive usage totals unchanged.
func (u *Usage) UnmarshalJSON(data []byte) error {
	type plainUsage Usage
	var base plainUsage
	wire := struct {
		*plainUsage
		InputTokenDetails openai.PromptTokensDetails `json:"input_tokens_details"`
	}{plainUsage: &base}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*u = Usage(base)
	u.InputTokenDetails.CacheWriteTokens = wire.InputTokenDetails.WriteCachedTokens
	u.InputTokenDetails.CachedTokens = wire.InputTokenDetails.CachedTokens
	u.InputTokenDetails.CachedTokensDetails = wire.InputTokenDetails.CachedTokensDetails
	return nil
}

func (u *Usage) ToUsage() *llm.Usage {
	return &llm.Usage{
		PromptTokens:     u.InputTokens,
		CompletionTokens: u.OutputTokens,
		TotalTokens:      u.TotalTokens,
		PromptTokensDetails: &llm.PromptTokensDetails{
			CachedTokens:        u.InputTokenDetails.CachedTokens,
			CachedTokensDetails: u.InputTokenDetails.CachedTokensDetails.Clone(),
			WriteCachedTokens:   u.InputTokenDetails.CacheWriteTokens,
		},
		CompletionTokensDetails: &llm.CompletionTokensDetails{
			ReasoningTokens: u.OutputTokenDetails.ReasoningTokens,
		},
	}
}

// ConvertLLMUsageToResponsesUsage converts llm.Usage to Responses API Usage.
func ConvertLLMUsageToResponsesUsage(usage *llm.Usage) *Usage {
	if usage == nil {
		return nil
	}

	result := &Usage{
		InputTokens:  usage.PromptTokens,
		OutputTokens: usage.CompletionTokens,
		TotalTokens:  usage.TotalTokens,
		Cost:         usage.Cost,
	}

	if usage.PromptTokensDetails != nil {
		result.InputTokenDetails.CachedTokens = usage.PromptTokensDetails.CachedTokens
		result.InputTokenDetails.CachedTokensDetails = usage.PromptTokensDetails.CachedTokensDetails.Clone()
		result.InputTokenDetails.CacheWriteTokens = usage.PromptTokensDetails.WriteCachedTokens
	}

	if usage.CompletionTokensDetails != nil {
		result.OutputTokenDetails.ReasoningTokens = usage.CompletionTokensDetails.ReasoningTokens
	}

	return result
}
