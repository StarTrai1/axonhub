package orchestrator

import (
	"context"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm"
)

// WebSearchSelector prefers Codex channels that support the standalone
// openai/search protocol. Compatible (auto) channels remain fallback candidates,
// while MCP-only Codex channels are never sent a standalone search request.
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

	eligible := lo.Filter(candidates, func(candidate *ChannelModelsCandidate, _ int) bool {
		return candidate == nil ||
			candidate.Channel == nil ||
			candidate.Channel.Type != channel.TypeCodex ||
			candidate.Channel.Policies.AllowsNativeWebSearchFallback()
	})

	capable := lo.Filter(eligible, func(candidate *ChannelModelsCandidate, _ int) bool {
		return candidate != nil &&
			candidate.Channel != nil &&
			candidate.Channel.Type == channel.TypeCodex &&
			candidate.Channel.Policies.PrefersNativeWebSearch()
	})
	if len(capable) == 0 {
		if log.DebugEnabled(ctx) {
			log.Debug(ctx, "no standalone web search channels, using compatible candidates",
				log.Int("candidate_count", len(candidates)),
				log.Int("eligible_candidate_count", len(eligible)))
		}

		return eligible, nil
	}

	if log.DebugEnabled(ctx) {
		log.Debug(ctx, "preferred standalone web search channels",
			log.Int("candidate_count", len(candidates)),
			log.Int("capable_candidate_count", len(capable)))
	}

	return capable, nil
}
