package llm

import "github.com/samber/lo"

// CachedTokensDetails preserves optional modality counts within cached_tokens.
// Missing values differ from explicitly reported zero and must not be inferred.
type CachedTokensDetails struct {
	TextTokens  *int64 `json:"text_tokens,omitempty"`
	ImageTokens *int64 `json:"image_tokens,omitempty"`
	AudioTokens *int64 `json:"audio_tokens,omitempty"`
}

// Clone keeps usage snapshots independent across protocol transformations.
func (d *CachedTokensDetails) Clone() *CachedTokensDetails {
	if d == nil {
		return nil
	}
	result := *d
	if d.TextTokens != nil {
		result.TextTokens = lo.ToPtr(*d.TextTokens)
	}
	if d.ImageTokens != nil {
		result.ImageTokens = lo.ToPtr(*d.ImageTokens)
	}
	if d.AudioTokens != nil {
		result.AudioTokens = lo.ToPtr(*d.AudioTokens)
	}
	return &result
}
