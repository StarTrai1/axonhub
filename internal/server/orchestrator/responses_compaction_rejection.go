package orchestrator

import (
	"context"
	"crypto/sha256"
	"time"

	"github.com/looplj/axonhub/internal/log"
)

const checkpointRejectionTTL = 30 * time.Minute

func (key responsesCompactionRecoveryKey) rejectionDigest() [sha256.Size]byte {
	// A reconnect can change the window while reusing exactly the same rejected
	// checkpoint. Keep owner, thread, model, destination and credential binding.
	key.scope.windowID = ""
	key.replay = false
	digest := key.digest()
	return sha256.Sum256(append([]byte("checkpoint-rejection-v1:"), digest[:]...))
}

func (m *responsesCompactionRecoveryMiddleware) rememberCheckpointRejection(ctx context.Context, key responsesCompactionRecoveryKey) {
	m.adapter.rememberCheckpointRejectionDigest(ctx, key.rejectionDigest())
}

func (a *remoteCompactionAdapter) rememberCheckpointRejectionDigest(ctx context.Context, digest [sha256.Size]byte) {
	if !a.unifiedHistoryEnabled() {
		return
	}
	if expiry, ok := a.checkpointRejections.Get(digest); ok && time.Now().Before(expiry) {
		return
	}
	expiry := time.Now().Add(checkpointRejectionTTL)
	a.checkpointRejections.Add(digest, expiry)
	// Persist the observed error even if the foreground is canceled before its
	// recovery finishes. This scope does not continue any generation.
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if err := a.systemService.SaveResponsesCompactionRejection(persistCtx, digest, expiry); err != nil {
		log.Warn(ctx, "could not retain native checkpoint rejection", log.Cause(err))
	}
}

func (m *responsesCompactionRecoveryMiddleware) hasCheckpointRejection(ctx context.Context, key responsesCompactionRecoveryKey) bool {
	return m.adapter.hasCheckpointRejectionDigest(ctx, key.rejectionDigest())
}

func (a *remoteCompactionAdapter) hasCheckpointRejectionDigest(ctx context.Context, digest [sha256.Size]byte) bool {
	if !a.unifiedHistoryEnabled() {
		return false
	}
	if expiry, ok := a.checkpointRejections.Get(digest); ok {
		if time.Now().Before(expiry) {
			return true
		}
		a.checkpointRejections.Remove(digest)
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	expiry, err := a.systemService.LoadResponsesCompactionRejection(lookupCtx, digest)
	if err != nil {
		log.Warn(ctx, "could not load native checkpoint rejection", log.Cause(err))
		return false
	}
	if !time.Now().Before(expiry) {
		return false
	}
	a.checkpointRejections.Add(digest, expiry)
	return true
}
