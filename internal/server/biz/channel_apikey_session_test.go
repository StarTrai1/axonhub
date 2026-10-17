package biz

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestTraceStickyKeyProvider_TransportSessionWithoutTrace(t *testing.T) {
	keys := []string{"credential-a", "credential-b", "credential-c", "credential-d"}
	ch := &Channel{Channel: &ent.Channel{Credentials: objects.ChannelCredentials{APIKeys: keys}}, cachedEnabledAPIKeys: keys}
	provider := NewTraceStickyKeyProvider(ch)
	selected := make(map[string]bool)
	for session := range 100 {
		ctx := shared.WithSessionID(shared.WithSessionScope(t.Context(), "api-key:1"), fmt.Sprintf("session-%d", session))
		key := provider.Get(ctx)
		selected[key] = true
		for range 20 {
			require.Equal(t, key, provider.Get(ctx), "turns in one transport session must keep their upstream credential without persisted tracing")
		}
		require.Equal(t, key, NewTraceStickyKeyProvider(ch).Get(ctx), "rebuilding an unchanged channel snapshot must preserve affinity")
	}
	require.Greater(t, len(selected), 1, "independent conversations must still distribute across credentials")
}

func TestTraceStickyKeyProvider_AffinityPrecedenceAndOwnership(t *testing.T) {
	ctx := shared.WithSessionID(shared.WithSessionScope(t.Context(), "api-key:1"), "same-client-session")
	seed := channelAPIKeyAffinitySeed(ctx)
	require.NotEmpty(t, seed)
	require.NotEqual(t, seed, channelAPIKeyAffinitySeed(shared.WithSessionScope(ctx, "api-key:2")))
	require.Empty(t, channelAPIKeyAffinitySeed(shared.WithSessionID(t.Context(), "unscoped-session")))
	require.Empty(t, channelAPIKeyAffinitySeed(context.Background()))

	ctx = contexts.WithThread(ctx, &ent.Thread{ID: 31})
	require.Equal(t, "thread:31", channelAPIKeyAffinitySeed(ctx))
	ctx = contexts.WithTrace(ctx, &ent.Trace{TraceID: "existing-trace-seed"})
	require.Equal(t, "existing-trace-seed", channelAPIKeyAffinitySeed(ctx), "keep existing trace hashing stable across upgrades")
}

func TestTraceStickyKeyProvider_DisabledCachedCredentialIsReplaced(t *testing.T) {
	keys := []string{"credential-a", "credential-b", "credential-c"}
	ch := &Channel{Channel: &ent.Channel{Credentials: objects.ChannelCredentials{APIKeys: keys}}, cachedEnabledAPIKeys: keys}
	provider := NewTraceStickyKeyProvider(ch)
	ctx := shared.WithSessionID(shared.WithSessionScope(t.Context(), "api-key:1"), "continuing-session")
	previous := provider.Get(ctx)
	ch.cachedEnabledAPIKeys = slices.DeleteFunc(slices.Clone(keys), func(key string) bool { return key == previous })
	next := provider.Get(ctx)
	require.NotEqual(t, previous, next)
	require.Contains(t, ch.cachedEnabledAPIKeys, next)
	require.Equal(t, next, provider.Get(ctx))
}
