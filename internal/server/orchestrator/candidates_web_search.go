package orchestrator

import (
	"context"


	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm"
)

// WebSearchSelector prefers Codex channels that support the standalone
// openai/search protocol. MCP-only channels are excluded unless explicitly
// reserved for standalone search.
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
	candidates = filterSearchOnlyCandidates(candidates, req)

	if len(candidates) == 0 || req == nil || req.APIFormat != llm.APIFormatOpenAISearch {
		return candidates, nil
	}

	preferred := false
	for _, candidate := range candidates {
		if candidate != nil && candidate.Channel != nil && candidate.Channel.Type == channel.TypeCodex && candidate.Channel.AllowsStandaloneSearch() {
			preferred = true
			break
		}
	}
	keep := func(candidate *ChannelModelsCandidate) bool {
		codex := candidate != nil && candidate.Channel != nil && candidate.Channel.Type == channel.TypeCodex
		if preferred {
			return codex && candidate.Channel.AllowsStandaloneSearch()
		}
		return !codex || candidate.Channel.AllowsStandaloneSearch()
	}
	// Most requests already contain only eligible Codex candidates. Reuse that
	// immutable slice; copy only when an exclusion is actually necessary.
	selected := candidates
	for i, candidate := range candidates {
		if keep(candidate) {
			continue
		}
		selected = make([]*ChannelModelsCandidate, 0, len(candidates)-1)
		selected = append(selected, candidates[:i]...)
		for _, remaining := range candidates[i+1:] {
			if keep(remaining) {
				selected = append(selected, remaining)
			}
		}
		break
	}
	if log.DebugEnabled(ctx) {
		log.Debug(ctx, "selected standalone web search channels",
			log.Int("candidate_count", len(candidates)),
			log.Int("eligible_candidate_count", len(selected)),
			log.Bool("preferred_codex", preferred))
	}
	return selected, nil
}
