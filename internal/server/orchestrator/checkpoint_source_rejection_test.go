package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestCheckpointSourceRejectionIsolationAndExpiry(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	state.RawProviderRequest = rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
	state.RawProviderRequest.URL = "https://relay.example/v1/responses"
	state.RawProviderRequest.Headers.Set("Authorization", "Bearer synthetic-credential")
	ref := &remoteCompactionReference{ID: "cmp_ancestor", EncryptedContent: "ancestor-cipher"}
	key, ok := checkpointSourceRecoveryKey(ctx, state, ref)
	require.True(t, ok)

	// Existing foreground rejection entries must not be promoted into source
	// rejection evidence simply because their hashes have the same owner.
	adapter.rememberCheckpointRejectionDigest(ctx, key.rejectionDigest())
	require.False(t, adapter.checkpointSourceWasRejected(ctx, state, ref))
	rejected := &compactionSourceRejectedError{cause: resourceMismatchHTTPError(), cacheKey: remoteCompactionCacheKey(ref), provider: key.scope.provider}
	rejected.provider.url = "https://different-summary.example/v1/responses"
	adapter.rememberCheckpointSourceRejection(ctx, state, ref, rejected)
	require.False(t, adapter.checkpointSourceWasRejected(ctx, state, ref), "an override selecting another destination cannot attest to this one")
	rejected.provider = key.scope.provider
	adapter.rememberCheckpointSourceRejection(ctx, state, ref, rejected)
	adapter = newRemoteCompactionAdapter(nil, nil, adapter.systemService)

	for _, change := range []string{"same", "window", "thread", "owner", "api key", "project", "credential", "endpoint", "model", "channel", "checkpoint id", "ciphertext", "missing identity", "mismatched owner", "multiple candidates"} {
		t.Run(change, func(t *testing.T) {
			next := *state
			next.RawProviderRequest = cloneRawRequest(state.RawProviderRequest)
			candidate := *state.ChannelModelsCandidates[0]
			wrapped := *candidate.Channel
			channel := *wrapped.Channel
			wrapped.Channel, candidate.Channel = &channel, &wrapped
			next.ChannelModelsCandidates = []*ChannelModelsCandidate{&candidate}
			nextRef, nextCtx := *ref, ctx
			switch change {
			case "window":
				next.RawProviderRequest.Headers.Set("X-Codex-Window-Id", "recovery-thread:999")
			case "thread":
				next.RawProviderRequest.Headers.Set("Thread-Id", "another-thread")
			case "owner":
				nextCtx = shared.WithSessionScope(ctx, "another-session-owner")
			case "api key", "project":
				owner := *state.APIKey
				if change == "api key" {
					owner.ID++
				} else {
					owner.ProjectID++
				}
				next.APIKey = &owner
				nextCtx = contexts.WithProjectID(contexts.WithAPIKey(contexts.CloneRequestContext(ctx), &owner), owner.ProjectID)
			case "credential":
				next.RawProviderRequest.Headers.Set("Authorization", "Bearer rotated-credential")
			case "endpoint":
				next.RawProviderRequest.URL = "https://another.example/v1/responses"
			case "model":
				var err error
				next.RawProviderRequest.Body, err = sjson.SetBytes(next.RawProviderRequest.Body, "model", "other-model")
				require.NoError(t, err)
			case "channel":
				channel.ID++
			case "checkpoint id":
				nextRef.ID = "cmp_another"
			case "ciphertext":
				nextRef.EncryptedContent = "replacement-cipher"
			case "missing identity":
				nextCtx = context.Background()
			case "mismatched owner":
				next.APIKey = &ent.APIKey{ID: state.APIKey.ID + 1, ProjectID: state.APIKey.ProjectID}
			case "multiple candidates":
				next.ChannelModelsCandidates = append(next.ChannelModelsCandidates, &ChannelModelsCandidate{Channel: &biz.Channel{}})
			}
			require.Equal(t, change == "same" || change == "window", adapter.checkpointSourceWasRejected(nextCtx, &next, &nextRef))
		})
	}
	expired := time.Now().Add(-time.Second)
	require.NoError(t, adapter.systemService.SaveResponsesCompactionRejection(ctx, checkpointSourceRejectionDigest(key), expired))
	adapter = newRemoteCompactionAdapter(nil, nil, adapter.systemService)
	require.False(t, adapter.checkpointSourceWasRejected(ctx, state, ref), "expiry restores the normal native attempt")
}
