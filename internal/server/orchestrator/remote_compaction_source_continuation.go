package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

// A native WebSocket compaction can contain only a delta and a connection-local
// response ID. Such a wire request is not a durable source. Resolve its exact
// completed predecessors while their request logs are still available, before
// publishing the checkpoint or migrating it to another provider.
func (a *remoteCompactionAdapter) resolveCompactionSourceContinuation(ctx context.Context, source *remoteCompactionSource, state *PersistenceState) (*remoteCompactionSource, error) {
	if source == nil || gjson.GetBytes(source.body, "previous_response_id").String() == "" {
		return source, nil
	}
	owner, ok := contexts.GetAPIKey(ctx)
	project, hasProject := contexts.GetProjectID(ctx)
	if !ok || !hasProject || owner == nil || state == nil || state.APIKey == nil ||
		owner.ID != state.APIKey.ID || project != state.APIKey.ProjectID || a.requestService == nil {
		return nil, compactionSourceContinuationUnavailable("authenticated predecessor lookup is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	envelope, input, err := decodeResponsesInput(source.body)
	if err != nil {
		return nil, err
	}
	previous := gjson.GetBytes(source.body, "previous_response_id").String()
	metadata := shared.ReadCodexRequestMetadata(source.headers, source.body)
	model := gjson.GetBytes(source.body, "model").String()
	segments := [][]json.RawMessage{input}
	bytes := len(source.body)
	seen := make(map[string]bool)
	for previous != "" {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if seen[previous] || len(seen) >= remoteCompactionHistoryLookupLimit {
			return nil, compactionSourceContinuationUnavailable("predecessor chain is cyclic or exceeds the lookup limit")
		}
		seen[previous] = true
		body, response, found, err := a.requestService.LoadCompletedResponsesCompactionPredecessor(ctx, previous)
		if err != nil {
			return nil, fmt.Errorf("load compaction predecessor: %w", err)
		}
		if !found {
			return nil, compactionSourceContinuationUnavailable("a completed predecessor is missing")
		}
		bytes += len(body) + len(response)
		if bytes > nativeCompactionMaxSourceBytes {
			return nil, compactionSourceContinuationUnavailable("predecessor chain exceeds the source size limit")
		}
		priorMetadata := shared.ReadCodexRequestMetadata(nil, body)
		if (metadata.ThreadID != "" && priorMetadata.ThreadID != "" && metadata.ThreadID != priorMetadata.ThreadID) ||
			(model != "" && gjson.GetBytes(body, "model").String() != model) {
			return nil, compactionSourceContinuationUnavailable("predecessor model or thread does not match the source")
		}
		_, priorInput, err := decodeResponsesInput(body)
		if err != nil {
			return nil, err
		}
		var completed struct {
			Output []json.RawMessage `json:"output"`
		}
		if err := json.Unmarshal(response, &completed); err != nil {
			return nil, err
		}
		segments = append(segments, completed.Output, priorInput)
		previous = gjson.GetBytes(body, "previous_response_id").String()
	}
	count := 0
	for _, segment := range segments {
		count += len(segment)
	}
	resolved := make([]json.RawMessage, 0, count)
	for i := len(segments)-1; i >= 0; i-- {
		resolved = append(resolved, segments[i]...)
	}
	if err := setResponseEnvelopeInput(envelope, resolved); err != nil {
		return nil, err
	}
	delete(envelope, "previous_response_id")
	body, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	if len(body) > nativeCompactionMaxSourceBytes {
		return nil, compactionSourceContinuationUnavailable("resolved source exceeds the source size limit")
	}
	return &remoteCompactionSource{body: body, headers: source.headers}, nil
}

func compactionSourceContinuationUnavailable(reason string) error {
	return &llm.ResponseError{StatusCode: http.StatusBadRequest, Detail: llm.ErrorDetail{
		Type: "invalid_request_error", Code: "compaction_history_unavailable", Param: "input",
		Message: "Retained compaction source could not be reconstructed: " + reason + "; restore the source history or use a compatible channel",
	}}
