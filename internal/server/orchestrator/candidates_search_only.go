package orchestrator

import (
	"context"
	"slices"

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
	filtered := make([]*ChannelModelsCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil || !candidate.Channel.IsCodexSearchOnly() {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

func (s *DefaultSelector) selectSearchOnlyCandidates(ctx context.Context, req *llm.Request) []*ChannelModelsCandidate {
	if !isStandaloneSearch(req) {
		return nil
	}
	var candidates []*ChannelModelsCandidate
	for _, ch := range s.ChannelService.GetEnabledChannels() {
		if !ch.IsCodexSearchOnly() {
			continue
		}
		entries := ch.GetModelEntries()
		entry, found := entries[req.Model]
		if !found {
			entry, found = entries[ch.DefaultTestModel]
		}
		if !found && len(entries) > 0 {
			keys := make([]string, 0, len(entries))
			for key := range entries {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			entry = entries[keys[0]]
			found = true
		}
		if !found {
			entry = biz.ChannelModelEntry{ActualModel: req.Model, Source: "search_only"}
		}
		entry.RequestModel = req.Model
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
