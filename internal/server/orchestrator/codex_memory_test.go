package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func codexMemoryTestContext(t *testing.T, key, project int) context.Context {
	t.Helper()
	return contexts.WithAPIKey(shared.WithResponsesAPI(t.Context()), &ent.APIKey{ID: key, ProjectID: project})
}

func codexMemoryTestRequest(thread, kind string) *httpclient.Request {
	return &httpclient.Request{
		Headers: http.Header{"Thread-Id": {"handshake-thread"}, "X-Codex-Turn-Metadata": {`{"request_kind":"prewarm"}`}},
		Body: []byte(fmt.Sprintf(`{"client_metadata":{"thread_id":%q,"x-codex-turn-metadata":%q},"input":"synthetic"}`, thread, `{"request_kind":"`+kind+`"}`)),
	}
}

func TestCodexMemoryGuardCancelsAndPausesOnlyMatchingConversation(t *testing.T) {
	guard := newCodexMemoryGuard()
	ctx := codexMemoryTestContext(t, 1, 2)
	foreground, err := guard.begin(ctx, codexMemoryTestRequest("thread", "turn"), func(error) {})
	require.NoError(t, err)
	for _, tc := range []struct {
		name, thread string
		key, project int
		canceled bool
	}{
		{"same conversation", "thread", 1, 2, true},
		{"other thread", "other", 1, 2, false},
		{"other key", "thread", 3, 2, false},
		{"other project", "thread", 1, 4, false},
	} {
		memoryCtx, cancel := context.WithCancelCause(codexMemoryTestContext(t, tc.key, tc.project))
		t.Cleanup(func() { cancel(nil) })
		lease, err := guard.begin(memoryCtx, codexMemoryTestRequest(tc.thread, "memory"), cancel)
		require.NoError(t, err)
		t.Cleanup(func() { lease.finish(false) })
		defer func() {
			require.Equal(t, tc.canceled, memoryCtx.Err() != nil, tc.name)
			if tc.canceled {
				require.ErrorIs(t, context.Cause(memoryCtx), errCodexMemoryPaused)
			}
		}()
	}
	foreground.finish(false)
	_, err = guard.begin(ctx, codexMemoryTestRequest("thread", "memory"), func(error) {})
	require.ErrorIs(t, err, errCodexMemoryPaused)
	foreground, err = guard.begin(ctx, codexMemoryTestRequest("thread", "turn"), func(error) {})
	require.NoError(t, err, "a foreground retry must remain allowed")
	foreground.finish(true)
	memory, err := guard.begin(ctx, codexMemoryTestRequest("thread", "memory"), func(error) {})
	require.NoError(t, err)
	memory.finish(true)
}

func TestCodexMemoryGuardExpiryAndStaleSuccess(t *testing.T) {
	guard := newCodexMemoryGuard()
	now := time.Now()
	guard.clock = func() time.Time { return now }
	ctx := codexMemoryTestContext(t, 1, 2)
	old, err := guard.begin(ctx, codexMemoryTestRequest("thread", "turn"), func(error) {})
	require.NoError(t, err)
	failed, err := guard.begin(ctx, codexMemoryTestRequest("thread", "turn"), func(error) {})
	require.NoError(t, err)
	failed.finish(false)
	old.finish(true)
	_, err = guard.begin(ctx, codexMemoryTestRequest("thread", "memory"), func(error) {})
	require.ErrorIs(t, err, errCodexMemoryPaused, "an older concurrent success must not clear the new failure")
	now = now.Add(codexMemoryPauseDuration)
	memory, err := guard.begin(ctx, codexMemoryTestRequest("thread", "memory"), func(error) {})
	require.NoError(t, err)
	memory.finish(false)
	require.Empty(t, guard.active)
	require.Zero(t, guard.paused.Len(), "memory failures must not renew the foreground pause")
}

func TestCodexMemoryGuardIgnoresUnrelatedRequests(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		value any
		key, project int
	}{
		{name: "unauthenticated key", key: 0, project: 1},
		{name: "unauthenticated project", key: 1, project: 0},
		{name: "subagent", key: 1, project: 1, path: "client_metadata.x-openai-subagent", value: "reviewer"},
		{name: "named agent", key: 1, project: 1, path: "client_metadata.x-codex-turn-metadata", value: `{"request_kind":"turn","agent_name":"/root/reviewer"}`},
		{name: "agent source", key: 1, project: 1, path: "client_metadata.x-codex-turn-metadata", value: `{"request_kind":"turn","thread_source":"subagent"}`},
		{name: "compaction", key: 1, project: 1, path: "client_metadata.x-codex-turn-metadata", value: `{"request_kind":"compaction"}`},
		{name: "prewarm", key: 1, project: 1, path: "client_metadata.x-codex-turn-metadata", value: `{"request_kind":"prewarm"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			guard := newCodexMemoryGuard()
			request := codexMemoryTestRequest("thread", "turn")
			var err error
			if tc.path != "" {
				request.Body, err = sjson.SetBytes(request.Body, tc.path, tc.value)
				require.NoError(t, err)
			}
			lease, err := guard.begin(codexMemoryTestContext(t, tc.key, tc.project), request, func(error) {})
			require.NoError(t, err)
			require.Nil(t, lease)
		})
	}
}

func TestCodexMemoryGuardFailureCacheIsBounded(t *testing.T) {
	guard := newCodexMemoryGuard()
	ctx := codexMemoryTestContext(t, 1, 2)
	for i := range 1100 {
		lease, err := guard.begin(ctx, codexMemoryTestRequest(fmt.Sprint(i), "turn"), func(error) {})
		require.NoError(t, err)
		lease.finish(false)
	}
	require.Equal(t, 1024, guard.paused.Len())
	require.Empty(t, guard.active)
}
