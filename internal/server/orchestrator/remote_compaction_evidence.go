package orchestrator

import (
	"sync"

	"github.com/looplj/axonhub/llm/httpclient"
)

// Evidence belongs to one recovery, including its native and text attempts.
// Flights can share it with waiters, so reads and writes are synchronized. It
// never becomes a channel-wide assertion about opaque state or capabilities.
type compactionRecoveryEvidence struct {
	mu         sync.Mutex
	rejections map[responsesMetadataCapabilityKey]map[string]struct{}
	statuses   map[responsesMetadataCapabilityKey]map[string]struct{}
}

func (e *compactionRecoveryEvidence) rejectSource(provider responsesMetadataCapabilityKey, cacheKey string) {
	if e == nil || provider.url == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.rejections == nil {
		e.rejections = make(map[responsesMetadataCapabilityKey]map[string]struct{})
	}
	if e.rejections[provider] == nil {
		e.rejections[provider] = make(map[string]struct{})
	}
	e.rejections[provider][cacheKey] = struct{}{}
}

func (e *compactionRecoveryEvidence) sourceRejected(state *PersistenceState, ref *remoteCompactionReference) bool {
	if e == nil || state == nil || state.RawProviderRequest == nil || ref == nil || len(state.ChannelModelsCandidates) != 1 {
		return false
	}
	candidate := state.ChannelModelsCandidates[0]
	if candidate == nil || candidate.Channel == nil {
		return false
	}
	provider := responsesMetadataKey(candidate.Channel.ID, state.RawProviderRequest)
	e.mu.Lock()
	defer e.mu.Unlock()
	_, found := e.rejections[provider][remoteCompactionCacheKey(ref)]
	return found
}

func (e *compactionRecoveryEvidence) rejectStatus(provider responsesMetadataCapabilityKey, rule responsesRejectedStatusRule) {
	// Never carry input indexes, reasoning removal or resource-affinity repair
	// across requests. Only a named item's explicitly unsupported status field
	// is a reusable schema correction on this exact destination.
	if e == nil || provider.url == "" || rule.itemType == "" || rule.fieldName() != "status" || rule.dropItem || rule.detachReasoningID {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.statuses == nil {
		e.statuses = make(map[responsesMetadataCapabilityKey]map[string]struct{})
	}
	if e.statuses[provider] == nil {
		e.statuses[provider] = make(map[string]struct{})
	}
	e.statuses[provider][rule.itemType] = struct{}{}
}

func (e *compactionRecoveryEvidence) statusRules(channelID int, request *httpclient.Request) []responsesRejectedStatusRule {
	if e == nil {
		return nil
	}
	provider := responsesMetadataKey(channelID, request)
	e.mu.Lock()
	defer e.mu.Unlock()
	rules := make([]responsesRejectedStatusRule, 0, len(e.statuses[provider]))
	for itemType := range e.statuses[provider] {
		rules = append(rules, responsesRejectedStatusRule{itemType: itemType, index: -1, field: "status", resourceScope: &provider})
	}
	return rules
}
