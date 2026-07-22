package orchestrator

import (
	"context"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm"
)

const (
	remoteCompactionTriggerType       = "compaction_trigger"
	remoteCompactionItemType          = "compaction"
	legacyRemoteCompactionSummaryType = "compaction_summary"
)

// RemoteCompactionSelector prefers explicitly capable Codex channels while
// preserving the existing candidate set when none are marked as capable.
type RemoteCompactionSelector struct {
	wrapped CandidateSelector
}

func WithRemoteCompactionSelector(wrapped CandidateSelector) *RemoteCompactionSelector {
	return &RemoteCompactionSelector{wrapped: wrapped}
}

func (s *RemoteCompactionSelector) Select(ctx context.Context, req *llm.Request) ([]*ChannelModelsCandidate, error) {
	candidates, err := s.wrapped.Select(ctx, req)
	if err != nil {
		return nil, err
	}

	if len(candidates) == 0 || !isRemoteCompactionRequest(req) {
		return candidates, nil
	}

	capable := lo.Filter(candidates, func(candidate *ChannelModelsCandidate, _ int) bool {
		return candidate != nil &&
			candidate.Channel != nil &&
			candidate.Channel.Type == channel.TypeCodex &&
			candidate.Channel.Policies.SupportsRemoteCompaction
	})
	if len(capable) == 0 {
		if log.DebugEnabled(ctx) {
			log.Debug(ctx, "no remote compaction capable channels, using all candidates",
				log.Int("candidate_count", len(candidates)))
		}

		return candidates, nil
	}

	if log.DebugEnabled(ctx) {
		log.Debug(ctx, "preferred remote compaction capable channels",
			log.Int("candidate_count", len(candidates)),
			log.Int("capable_candidate_count", len(capable)))
	}

	return capable, nil
}

func isRemoteCompactionRequest(req *llm.Request) bool {
	if req == nil {
		return false
	}

	// The legacy remote compaction protocol uses POST /responses/compact.
	if req.RequestType == llm.RequestTypeCompact {
		return true
	}

	// Remote compaction v2 uses an ordinary Responses request with a raw
	// compaction_trigger item, then installs the returned opaque compaction item
	// into subsequent turns. The inbound transformer normalizes the latter into
	// message content, so both representations must be checked.
	if req.APIFormat != llm.APIFormatOpenAIResponse {
		return false
	}

	for _, message := range req.Messages {
		for _, part := range message.Content.MultipleContent {
			switch part.Type {
			case remoteCompactionItemType, legacyRemoteCompactionSummaryType:
				return true
			}
		}
	}

	// Feature-advertisement headers alone do not mean that a specific request
	// needs remote compaction support.
	if req.ProviderExtensions == nil ||
		req.ProviderExtensions.OpenAIResponses == nil ||
		req.ProviderExtensions.OpenAIResponses.Request == nil {
		return false
	}

	for _, item := range req.ProviderExtensions.OpenAIResponses.Request.RawInputItems {
		switch item.Type {
		case remoteCompactionTriggerType,
			remoteCompactionItemType,
			legacyRemoteCompactionSummaryType:
			return true
		}
	}

	return false
}
