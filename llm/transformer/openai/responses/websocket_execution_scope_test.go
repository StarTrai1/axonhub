package responses

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestWebSocketExecutorSeparatesCodexExecutionScopes(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		thread string
		kind   string
	}{
		{name: "child thread", thread: "child", kind: "turn"},
		{name: "background memory", thread: "parent", kind: "memory"},
		{name: "future background kind", thread: "parent", kind: "guardian"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var upgrades atomic.Int32
			upgrader := websocket.Upgrader{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					t.Errorf("upgrade: %v", err)
					return
				}
				defer conn.Close()
				upgrades.Add(1)
				var payload map[string]json.RawMessage
				if err := conn.ReadJSON(&payload); err != nil {
					t.Errorf("read request: %v", err)
					return
				}
				if err := conn.WriteJSON(map[string]any{
					"type": "response.completed",
					"response": map[string]any{
						"id": "resp_scope", "status": "completed", "output": []any{},
					},
				}); err != nil {
					t.Errorf("write response: %v", err)
				}
			}))
			defer server.Close()

			executor := NewWebSocketExecutor(nil)
			defer executor.Close()
			makeRequest := func(thread, kind string) *httpclient.Request {
				metadata, err := json.Marshal(map[string]string{
					"session_id": "root-session", "thread_id": thread, "request_kind": kind,
				})
				require.NoError(t, err)
				body, err := json.Marshal(map[string]any{
					"model": "gpt-6-astra", "input": []any{},
					"client_metadata": map[string]string{"x-codex-turn-metadata": string(metadata)},
				})
				require.NoError(t, err)
				return &httpclient.Request{
					URL:     server.URL + "/v1/responses",
					Headers: http.Header{webSocketSessionHeader: []string{"root-session"}},
					Auth:    &httpclient.AuthConfig{Type: httpclient.AuthTypeBearer, APIKey: "test-key"},
					Body:    body,
				}
			}

			parent, err := executor.DoStream(webSocketTestContext(), makeRequest("parent", "turn"))
			require.NoError(t, err)
			defer parent.Close()
			// Keep the parent lease occupied. A child must not wait for the parent
			// response to finish before its own tool work can make progress.
			ctx, cancel := context.WithTimeout(webSocketTestContext(), 3*time.Second)
			defer cancel()
			child, err := executor.DoStream(ctx, makeRequest(testCase.thread, testCase.kind))
			require.NoError(t, err)
			defer child.Close()
			require.True(t, child.Next())
			require.Equal(t, "response.completed", child.Current().Type)
			require.False(t, child.Next())
			require.NoError(t, child.Err())

			require.True(t, parent.Next())
			require.False(t, parent.Next())
			require.NoError(t, parent.Err())
			require.Equal(t, int32(2), upgrades.Load())
		})
	}
}

func TestCodexExecutionPoolIdentityPreservesSequentialReuse(t *testing.T) {
	t.Parallel()

	base := codexExecutionPoolIdentity(shared.CodexRequestMetadata{ThreadID: "thread", RequestKind: "turn"})
	for _, kind := range []string{"", "turn", "prewarm", "compaction"} {
		metadata := shared.CodexRequestMetadata{ThreadID: "thread", RequestKind: kind, WindowID: "thread:99"}
		require.Equal(t, base, codexExecutionPoolIdentity(metadata))
	}
	require.NotEqual(t, base, codexExecutionPoolIdentity(shared.CodexRequestMetadata{ThreadID: "other", RequestKind: "turn"}))
	require.NotEqual(t, base, codexExecutionPoolIdentity(shared.CodexRequestMetadata{ThreadID: "thread", RequestKind: "memory"}))
	require.NotEqual(t, base, codexExecutionPoolIdentity(shared.CodexRequestMetadata{ThreadID: "thread", Subagent: "guardian"}))
	require.Empty(t, codexExecutionPoolIdentity(shared.CodexRequestMetadata{}))
}
