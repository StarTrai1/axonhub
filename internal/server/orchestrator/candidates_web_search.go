package orchestrator

import (
	"context"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm"
)

// WebSearchSelector prefers Codex channels that support the standalone
// openai/search protocol. If none are capable, it preserves all eligible
// candidates so existing load-balancing and fallback behavior remains intact.
type WebSearchSelector struct {
	wrapped CandidateSelector
}

func WithWebSearchSelector(wrapped CandidateSelector) *WebSearchSelector {
	return &WebSearchSelector{wrapped: wrapped}
}

func (s *WebSearchSelector) Select(ctx context.Context, req *llm.Request) ([]*ChannelModelsCandidate, error) {
	candidates, err := s.wrapped.Select(ctx, req)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 || req == nil || req.APIFormat != llm.APIFormatOpenAISearch {
		return candidates, nil
	}

	capable := lo.Filter(candidates, func(candidate *ChannelModelsCandidate, _ int) bool {
		return candidate != nil &&
			candidate.Channel != nil &&
			candidate.Channel.Type == channel.TypeCodex &&
			candidate.Channel.Policies.SupportsWebSearchRequests()
	})
	if len(capable) == 0 {
		if log.DebugEnabled(ctx) {
			log.Debug(ctx, "no web search capable channels, using all candidates",
				log.Int("candidate_count", len(candidates)))
		}

		return candidates, nil
	}

	if log.DebugEnabled(ctx) {
		log.Debug(ctx, "preferred web search capable channels",
			log.Int("candidate_count", len(candidates)),
			log.Int("capable_candidate_count", len(capable)))
	}

	return capable, nil
}
