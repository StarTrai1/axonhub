package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

const unifiedSummaryFailureTTL = time.Minute

type unifiedSummaryFailureKey struct {
	owner string
	destination [sha256.Size]byte
}

func unifiedSegmentFailureKey(state *PersistenceState, segment *unifiedHistorySegment) unifiedSummaryFailureKey {
	key := segment.cacheAlias
	if key == "" { key = segment.key }
	result := unifiedSummaryFailureKey{owner: remoteCompactionOwnerCacheKey(state, key)}
	candidate := state.CurrentCandidate
	if candidate == nil && len(state.ChannelModelsCandidates) > 0 { candidate = state.ChannelModelsCandidates[0] }
	if candidate != nil && candidate.Channel != nil {
		// Hash configuration and credentials; keep neither secrets nor request
		// bodies in the bounded failure cache. A changed destination can retry.
		encoded, _ := json.Marshal([]any{candidate.Channel.ID, candidate.Channel.BaseURL, candidate.Channel.Settings, candidate.Channel.Credentials, candidate.APIFormat})
		result.destination = sha256.Sum256(encoded)
	}
	return result
}

func (a *remoteCompactionAdapter) unifiedSummaryCooldown(key unifiedSummaryFailureKey, now time.Time) error {
	if a.summaryFailures == nil { return nil }
	until, found := a.summaryFailures.Get(key)
	if !found { return nil }
	if !now.Before(until) { a.summaryFailures.Remove(key); return nil }
	seconds := max(1, int((until.Sub(now)+time.Second-1)/time.Second))
	return &llm.ResponseError{StatusCode: http.StatusServiceUnavailable, Detail: llm.ErrorDetail{
		Type: "upstream_error", Code: "unified_history_preparation_cooldown",
		Message: fmt.Sprintf("This history segment failed to prepare on the selected channel. No new summary was requested; retry after %d seconds or use another compatible channel.", seconds),
	}, Cause: &httpclient.Error{StatusCode: http.StatusServiceUnavailable, Headers: http.Header{"Retry-After": {strconv.Itoa(seconds)}}}}
}

func (a *remoteCompactionAdapter) rememberUnifiedSummaryFailure(ctx context.Context, key unifiedSummaryFailureKey, err error) {
	if a.summaryFailures != nil && err != nil && ctx.Err() == nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		a.summaryFailures.Add(key, time.Now().Add(unifiedSummaryFailureTTL))
	}
}
