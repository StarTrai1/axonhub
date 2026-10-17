package orchestrator

import (
	"context"

	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
)

func isStandaloneSearch(req *llm.Request) bool {
	return req != nil && req.RequestType == llm.RequestTypeSearch && req.APIFormat == llm.APIFormatOpenAISearch
}

func filterSearchOnlyCandidates(candidates []*ChannelModelsCandidate, req *llm.Request) []*ChannelModelsCandidate {
	if isStandaloneSearch(req) {
		return candidates
	}
	for i, candidate := range candidates {
		if candidate != nil && candidate.Channel.IsCodexSearchOnly() {
			filtered := make([]*ChannelModelsCandidate, 0, len(candidates)-1)
			filtered = append(filtered, candidates[:i]...)
			for _, remaining := range candidates[i+1:] {
				if remaining == nil || !remaining.Channel.IsCodexSearchOnly() {
					filtered = append(filtered, remaining)
				}
			}
			return filtered
		}
	}
	return candidates
}

func (s *DefaultSelector) selectSearchOnlyCandidates(ctx context.Context, req *llm.Request) []*ChannelModelsCandidate {
	if !isStandaloneSearch(req) {
		return nil
	}
	var candidates []*ChannelModelsCandidate
	for _, ch := range s.ChannelService.GetEnabledChannels() {
		if !ch.IsCodexSearchOnly() || !ch.AllowsStandaloneSearch() {
			continue
		}
		model := ch.Settings.CodexSearchModel
		if model == "" {
			model = req.Model
		}
		if !objects.IsCodexSearchModel(model) {
			continue
		}
		entry := biz.ChannelModelEntry{RequestModel: req.Model, ActualModel: model, Source: "search_only"}
		endpoints := applyForcedAPIFormatsForRequest(ctx, ch, []biz.ChannelModelEntry{entry}, req.Model, req.RequestType, ch.ResolveEndpoints())
		format := SelectAPIFormat(endpoints, req)
		if format != llm.APIFormatOpenAISearch.String() {
			continue
		}
		candidates = append(candidates, &ChannelModelsCandidate{
			Channel: ch, Models: []biz.ChannelModelEntry{entry}, APIFormat: format,
		})
	}
	return candidates
}
