package orchestrator

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestResponsesSessionIdentityRetainsScopeAndExpiry(t *testing.T) {
	store := newResponsesSessionStore()
	ctx := shared.WithSessionScope(t.Context(), "api-key:owner")
	key := responsesSessionKey{scope: "api-key:owner", responseID: "response"}
	store.insertLocked(key, &responsesSessionRecord{
		input:     []json.RawMessage{[]byte(`{"role":"user","content":"retained"}`)},
		sessionID: "session", windowID: "window", updatedAt: time.Now(), complete: true,
	})
	session, window, found := store.lookupIdentity(ctx, "response")
	require.True(t, found)
	require.Equal(t, "session", session)
	require.Equal(t, "window", window)
	_, _, found = store.lookupIdentity(shared.WithSessionScope(ctx, "api-key:other"), "response")
	require.False(t, found)
	_, _, found = store.lookupIdentity(t.Context(), "response")
	require.False(t, found)
	snapshot := store.lookup(ctx, "response")
	snapshot.input[0][0] = 'x'
	require.Equal(t, byte('{'), store.lookup(ctx, "response").input[0][0], "full replay snapshots must still be isolated")
	store.insertLocked(key, &responsesSessionRecord{sessionID: "expired", updatedAt: time.Now().Add(-responsesSessionTTL - time.Second)})
	_, _, found = store.lookupIdentity(ctx, "response")
	require.False(t, found)
	_, _, found = store.lookupIdentity(ctx, "missing")
	require.False(t, found)
}

func TestResponsesContinuationReusesCompleteSnapshotAndHonorsWindow(t *testing.T) {
	for _, window := range []string{"", "window-1", "window-2"} {
		t.Run("window="+window, func(t *testing.T) {
			ctx := shared.WithSessionScope(t.Context(), "owner")
			store := newResponsesSessionStore()
			store.record(ctx, []byte(`{"client_metadata":{"x-codex-window-id":"window-1"},"input":[{"role":"user","content":"old history"}]}`), []byte(`{"id":"previous","status":"completed","output":[]}`))
			body, err := json.Marshal(map[string]any{
				"previous_response_id": "previous", "input": "new turn",
				"client_metadata": map[string]string{"x-codex-window-id": window},
			})
			require.NoError(t, err)
			c := newResponsesContinuation(store, body)
			p := &PersistentOutboundTransformer{state: &PersistenceState{ResponsesContinuation: c}}
			require.NoError(t, selectResponsesContinuationCandidate(ctx, p, &llm.Request{}))
			owner := c.owner
			require.NotNil(t, owner.Snapshot)
			prepared, err := prepareResponsesAttemptBody(ctx, p, responsesAttemptTarget{})
			require.NoError(t, err)
			require.Contains(t, string(prepared), "new turn")
			if window == "window-2" {
				require.NotContains(t, string(prepared), "old history")
				require.Nil(t, c.owner)
			} else {
				require.Contains(t, string(prepared), "old history")
				require.Same(t, owner, c.owner, "preparation must reuse the authenticated complete snapshot")
			}
		})
	}
}

func BenchmarkResponsesSessionIdentity(b *testing.B) {
	store := newResponsesSessionStore()
	ctx := shared.WithSessionScope(b.Context(), "owner")
	store.insertLocked(responsesSessionKey{scope: "owner", responseID: "response"}, &responsesSessionRecord{
		input:     []json.RawMessage{bytes.Repeat([]byte("x"), 8<<20)},
		sessionID: "session", windowID: "window", updatedAt: time.Now(), size: 8 << 20, complete: true,
	})
	for _, variant := range []string{"full_snapshot_baseline", "identity"} {
		b.Run(variant, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if variant == "full_snapshot_baseline" {
					if record := store.lookup(ctx, "response"); record == nil || record.sessionID != "session" {
						b.Fatal("missing session")
					}
				} else {
					if session, _, found := store.lookupIdentity(ctx, "response"); !found || session != "session" {
						b.Fatal("missing session")
					}
				}
			}
		})
	}
}
