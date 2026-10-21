package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
)

const checkpointSummaryMaxBytes = 64 << 10

// Legacy native checkpoints can be migrated from retained source records, but
// only during this explicit conversion. Existing portable checkpoints stop the
// traversal: their summary, rather than their pre-compaction archive, is used.
func (a *remoteCompactionAdapter) restoreCheckpointWindow(ctx context.Context, source *remoteCompactionSource, state *PersistenceState) (*remoteCompactionSource, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	body := source.body
	seen := make(map[string]bool)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ref, _, _, err := parseRemoteCompactionRequest(body)
		if err != nil {
			return nil, err
		}
		if ref == nil {
			return &remoteCompactionSource{body: body, headers: source.headers}, nil
		}
		key := remoteCompactionCacheKey(ref)
		if seen[key] || len(seen) >= remoteCompactionHistoryLookupLimit || len(body) > nativeCompactionMaxSourceBytes {
			return nil, checkpointSummaryError("compaction_source_invalid", "Checkpoint source is cyclic or exceeds the migration limit")
		}
		seen[key] = true
		if isSealedLocalCompactionReference(ref) {
			summary, err := a.openCompactionCheckpoint(ctx, ref, state, ref.EncryptedContent)
			if err != nil {
				return nil, err
			}
			body, err = replaceRemoteCompactionWithLocalSummary(body, summary)
			if err != nil {
				return nil, err
			}
			continue
		}
		retained, err := a.loadNativeCompactionSource(ctx, ref, state)
		if err != nil {
			return nil, err
		}
		if retained == nil {
			return nil, checkpointSummaryError("compaction_history_unavailable", "The native checkpoint has no retained source for portable migration; continue with its original resource")
		}
		expanded, complete, err := spliceUnifiedCompactionSource(body, retained.body, ref)
		if err != nil {
			return nil, err
		}
		if !complete {
			return nil, checkpointSummaryError("compaction_source_invalid", "The retained checkpoint source does not match the current window")
		}
		body = expanded
	}
}

// A migration can read a retained source once. It cannot publish that source as
// a compacted prompt, split it into model requests, or truncate it to fit.
func prepareCheckpointSummarySource(ctx context.Context, source *remoteCompactionSource, state *PersistenceState) (*remoteCompactionSource, error) {
	if source == nil {
		return nil, checkpointSummaryError("compaction_history_unavailable", "Checkpoint source is unavailable")
	}
	body := source.body
	ref, _, _, err := parseRemoteCompactionRequest(body)
	if err != nil {
		return nil, err
	}
	if ref == nil {
		envelope, input, err := decodeResponsesInput(body)
		if err != nil {
			return nil, err
		}
		portable, _, supported := unifiedPortableItems(input)
		if !supported {
			return nil, checkpointSummaryError("compaction_source_not_portable", "Checkpoint source contains state that cannot be safely represented as text")
		}
		if err := setResponseEnvelopeInput(envelope, portable); err != nil {
			return nil, err
		}
		body, err = json.Marshal(envelope)
		if err != nil {
			return nil, err
		}
	}
	body, err = protectUnifiedHistory(ctx, body, state)
	if err != nil {
		return nil, err
	}
	var candidate *ChannelModelsCandidate
	if state != nil && len(state.ChannelModelsCandidates) > 0 {
		candidate = state.ChannelModelsCandidates[0]
	}
	if candidate != nil && len(candidate.Models) > 0 {
		contextTokens := candidate.ContextTokens
		if contextTokens <= 0 {
			contextTokens = biz.BundledModelContextLimit(candidate.Models[0].ActualModel)
		}
		if contextTokens > 0 {
			_, budget := unifiedHistoryInputBudgets(body, candidate, 0, contextTokens)
			if unifiedContextSize(body) > budget {
				return nil, checkpointSummaryError("compaction_source_too_large", "Checkpoint source exceeds the selected model's input budget; no summary generation was started")
			}
		}
	}
	return &remoteCompactionSource{body: body, headers: source.headers}, nil
}

func checkpointSummaryError(code, message string) error {
	return &llm.ResponseError{StatusCode: http.StatusBadRequest, Detail: llm.ErrorDetail{Type: "invalid_request_error", Code: code, Param: "input", Message: message}}
}

func validateCheckpointSummary(summary string) error {
	if len(summary) > checkpointSummaryMaxBytes {
		return checkpointSummaryError("compaction_summary_too_large", "The generated checkpoint exceeds the compact summary budget; the previous context remains authoritative")
	}
	return nil
}
