package responses

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestWebSocketStatelessPreservesFullHistoryOnReusedConnection(t *testing.T) {
	var connections atomic.Int32
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections.Add(1)
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer conn.Close()
		for i := range 2 {
			var payload map[string]any
			require.NoError(t, conn.ReadJSON(&payload))
			require.NotContains(t, payload, "previous_response_id")
			require.NotContains(t, payload, "ResponsesStateless")
			require.Len(t, payload["input"], i+1)
			require.NoError(t, conn.WriteJSON(map[string]any{
				"type": "response.completed", "response": map[string]any{
					"id": fmt.Sprintf("resp_%d", i+1), "status": "completed", "output": []any{},
				},
			}))
		}
	}))
	defer server.Close()
	executor := NewWebSocketExecutor(nil)
	ctx := webSocketTestContext()
	for _, body := range []string{
		`{"model":"gpt-test","input":[{"role":"user","content":"first"}]}`,
		`{"model":"gpt-test","input":[{"role":"user","content":"first"},{"role":"user","content":"next"}]}`,
	} {
		stream, err := executor.DoStream(ctx, &httpclient.Request{
			Method: http.MethodPost, URL: server.URL + "/v1/responses", Body: []byte(body), ResponsesStateless: true,
			Headers: http.Header{webSocketSessionHeader: {"stateless-history"}},
		})
		require.NoError(t, err)
		require.True(t, stream.Next())
		require.Equal(t, "response.completed", stream.Current().Type)
		require.False(t, stream.Next())
		require.NoError(t, stream.Err())
		require.NoError(t, stream.Close())
	}
	require.Equal(t, int32(1), connections.Load())
}

func TestWebSocketStatelessRejectsUnresolvedID(t *testing.T) {
	payload := map[string]any{"previous_response_id": "resource-bound", "input": "delta"}
	err := prepareWebSocketPayloadForLease(payload, &webSocketLease{stateless: true})
	require.ErrorIs(t, err, shared.ErrResponsesContinuationUnavailable)
	require.Equal(t, "resource-bound", payload["previous_response_id"])
}
