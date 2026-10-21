package orchestrator

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/looplj/axonhub/llm/transformer/shared"
)

// Once a route rejects its conversation's native checkpoint, future compactions
// on that route must be portable. A new thread or changed channel configuration
// starts a new contract. This records observed incompatibility, not a probe.
func portableCheckpointSessionKey(state *PersistenceState, candidate *ChannelModelsCandidate) string {
	if state == nil || state.APIKey == nil || state.RawRequest == nil || candidate == nil || candidate.Channel == nil || len(candidate.Models) == 0 {
		return ""
	}
	metadata := shared.ReadCodexRequestMetadata(state.RawRequest.Headers, rawRequestPayload(state.RawRequest))
	if metadata.ThreadID == "" {
		return ""
	}
	identity, _ := json.Marshal([]any{
		"portable-checkpoint-session-v1", state.APIKey.ProjectID, state.APIKey.ID,
		metadata.ThreadID, candidate.Channel.ID, candidate.Channel.UpdatedAt, candidate.Channel.BaseURL, candidate.Models[0].ActualModel,
	})
	return "view:" + historyDigest(identity)
}

func (a *remoteCompactionAdapter) usesPortableCheckpoint(ctx context.Context, state *PersistenceState, candidate *ChannelModelsCandidate) (bool, error) {
	if a == nil || a.systemService == nil {
		return false, nil
	}
	key := portableCheckpointSessionKey(state, candidate)
	if key == "" {
		return false, nil
	}
	entries, err := a.systemService.LoadUnifiedHistory(ctx, []string{key})
	if err != nil || entries[key] == "" {
		return false, err
	}
	value, err := a.openHistoryRecord(ctx, key, entries[key], 128)
	if err != nil {
		return false, err
	}
	if string(value) != "portable-checkpoint-session-v1" {
		return false, errors.New("invalid checkpoint session contract")
	}
	return true, nil
}

func (a *remoteCompactionAdapter) requirePortableCheckpoint(ctx context.Context, state *PersistenceState) error {
	if a == nil || a.systemService == nil {
		return nil
	}
	key := portableCheckpointSessionKey(state, state.CurrentCandidate)
	if key == "" {
		return nil
	}
	value, err := a.sealHistoryRecord(ctx, key, []byte("portable-checkpoint-session-v1"))
	if err != nil {
		return err
	}
	return a.systemService.SaveUnifiedHistory(ctx, map[string]string{key: value})
}
