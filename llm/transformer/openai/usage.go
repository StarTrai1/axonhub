package openai

import (
	"encoding/json"

	"github.com/looplj/axonhub/llm"
)

// PromptTokensDetails Breakdown of tokens used in the prompt.
type PromptTokensDetails struct {
	AudioTokens  int64 `json:"audio_tokens"`
	CachedTokens int64 `json:"cached_tokens"`
	// WriteCachedTokens is the number of prompt tokens written to the cache.
	WriteCachedTokens int64 `json:"cache_write_tokens,omitempty"`
}

// UnmarshalJSON accepts the official OpenAI field and compatibility aliases
// emitted by older or third-party OpenAI-compatible gateways.
func (d *PromptTokensDetails) UnmarshalJSON(data []byte) error {
	var wire struct {
		AudioTokens          int64  `json:"audio_tokens"`
		CachedTokens         int64  `json:"cached_tokens"`
		CacheWriteTokens     *int64 `json:"cache_write_tokens"`
		WriteCachedTokens    *int64 `json:"write_cached_tokens"`
		CacheCreationTokens  *int64 `json:"cache_creation_tokens"`
		CachedCreationTokens *int64 `json:"cached_creation_tokens"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	d.AudioTokens = wire.AudioTokens
	d.CachedTokens = wire.CachedTokens
	d.WriteCachedTokens = 0
	for _, value := range []*int64{
		wire.CacheWriteTokens,
		wire.WriteCachedTokens,
		wire.CacheCreationTokens,
		wire.CachedCreationTokens,
	} {
		if value != nil {
			d.WriteCachedTokens = *value

			break
		}
	}

	return nil
}

// CompletionTokensDetails Breakdown of tokens used in a completion.
type CompletionTokensDetails struct {
	AudioTokens              int64 `json:"audio_tokens"`
	ReasoningTokens          int64 `json:"reasoning_tokens"`
	AcceptedPredictionTokens int64 `json:"accepted_prediction_tokens"`
	RejectedPredictionTokens int64 `json:"rejected_prediction_tokens"`
}

// Usage represents the usage response from OpenAI compatible format.
// Difference provider may have different format, so we use this to convert to unified format.
type Usage struct {
	PromptTokens            int64                   `json:"prompt_tokens"`
	CompletionTokens        int64                   `json:"completion_tokens"`
	TotalTokens             int64                   `json:"total_tokens"`
	PromptTokensDetails     PromptTokensDetails     `json:"prompt_tokens_details"`
	CompletionTokensDetails CompletionTokensDetails `json:"completion_tokens_details"`

	// CachedTokens is the number of tokens that were cached for Moonshot.
	CachedTokens int64 `json:"cached_tokens,omitempty"`
}

func (u *Usage) ToLLMUsage() *llm.Usage {
	if u == nil {
		return nil
	}

	usage := &llm.Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
	}

	if u.PromptTokensDetails != (PromptTokensDetails{}) {
		usage.PromptTokensDetails = &llm.PromptTokensDetails{
			AudioTokens:       u.PromptTokensDetails.AudioTokens,
			CachedTokens:      u.PromptTokensDetails.CachedTokens,
			WriteCachedTokens: u.PromptTokensDetails.WriteCachedTokens,
		}
	}

	if u.CompletionTokensDetails != (CompletionTokensDetails{}) {
		usage.CompletionTokensDetails = &llm.CompletionTokensDetails{
			AudioTokens:              u.CompletionTokensDetails.AudioTokens,
			ReasoningTokens:          u.CompletionTokensDetails.ReasoningTokens,
			AcceptedPredictionTokens: u.CompletionTokensDetails.AcceptedPredictionTokens,
			RejectedPredictionTokens: u.CompletionTokensDetails.RejectedPredictionTokens,
		}
	}

	if (usage.PromptTokensDetails == nil || usage.PromptTokensDetails.CachedTokens == 0) && u.CachedTokens > 0 {
		if usage.PromptTokensDetails == nil {
			usage.PromptTokensDetails = &llm.PromptTokensDetails{}
		}

		usage.PromptTokensDetails.CachedTokens = u.CachedTokens
	}

	return usage
}

// UsageFromLLM creates OpenAI Usage from unified llm.Usage.
func UsageFromLLM(u *llm.Usage) *Usage {
	if u == nil {
		return nil
	}

	usage := &Usage{
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
	}

	if u.PromptTokensDetails != nil {
		usage.PromptTokensDetails = PromptTokensDetails{
			AudioTokens:       u.PromptTokensDetails.AudioTokens,
			CachedTokens:      u.PromptTokensDetails.CachedTokens,
			WriteCachedTokens: u.PromptTokensDetails.WriteCachedTokens,
		}
	}

	if u.CompletionTokensDetails != nil {
		usage.CompletionTokensDetails = CompletionTokensDetails{
			AudioTokens:              u.CompletionTokensDetails.AudioTokens,
			ReasoningTokens:          u.CompletionTokensDetails.ReasoningTokens,
			AcceptedPredictionTokens: u.CompletionTokensDetails.AcceptedPredictionTokens,
			RejectedPredictionTokens: u.CompletionTokensDetails.RejectedPredictionTokens,
		}
	}

	return usage
}
