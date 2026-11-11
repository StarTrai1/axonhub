package orchestrator

import (
	"context"
	"crypto/sha256"
)

// A canceled migration must not retry every rejected ancestor on reconnect.
// Bind evidence to the authenticated owner, thread, exact checkpoint and final
// destination. The context window may change while the checkpoint stays the same.
func checkpointSourceRecoveryKey(ctx context.Context, state *PersistenceState, ref *remoteCompactionReference) (responsesCompactionRecoveryKey, bool) {
	if state == nil || state.APIKey == nil || state.RawProviderRequest == nil ||
		ref == nil || ref.ID == "" || ref.EncryptedContent == "" || isLocalCompactionReference(ref) ||
		len(state.ChannelModelsCandidates) != 1 {
		return responsesCompactionRecoveryKey{}, false
	}
	owner, err := historyState(ctx)
	if err != nil || owner.APIKey.ID != state.APIKey.ID || owner.APIKey.ProjectID != state.APIKey.ProjectID {
		return responsesCompactionRecoveryKey{}, false
	}
	candidate := state.ChannelModelsCandidates[0]
	if candidate == nil {
		return responsesCompactionRecoveryKey{}, false
	}
	scope, ok := responsesReasoningScope(ctx, candidate.Channel, state.RawProviderRequest)
	if !ok {
		return responsesCompactionRecoveryKey{}, false
	}
	return responsesCompactionRecoveryKey{
		scope: scope, cacheKey: remoteCompactionCacheKey(ref), threadID: scope.threadID,
		apiKeyID: state.APIKey.ID, projectID: state.APIKey.ProjectID,
	}, true
}

func checkpointSourceRejectionDigest(key responsesCompactionRecoveryKey) [sha256.Size]byte {
	// Source restoration is separate from foreground checkpoint replacement.
	// Reuse the versioned expiry store without interpreting old records as this
	// stronger evidence or storing any prompt or credential bytes.
	digest := key.rejectionDigest()
	return sha256.Sum256(append([]byte("checkpoint-source-rejection-v1:"), digest[:]...))
}

func (a *remoteCompactionAdapter) checkpointSourceWasRejected(ctx context.Context, state *PersistenceState, ref *remoteCompactionReference) bool {
	key, ok := checkpointSourceRecoveryKey(ctx, state, ref)
	return ok && a.hasCheckpointRejectionDigest(ctx, checkpointSourceRejectionDigest(key))
}

func (a *remoteCompactionAdapter) rememberCheckpointSourceRejection(ctx context.Context, state *PersistenceState, ref *remoteCompactionReference, rejected *compactionSourceRejectedError) {
	key, ok := checkpointSourceRecoveryKey(ctx, state, ref)
	if !ok || rejected == nil || rejected.cacheKey != key.cacheKey || rejected.provider != key.scope.provider {
		return
	}
	// Only the actual summary route may attest to this destination. Overrides,
	// credential changes or another flight's failure cannot grant this evidence.
	a.rememberCheckpointRejectionDigest(ctx, checkpointSourceRejectionDigest(key))
}
