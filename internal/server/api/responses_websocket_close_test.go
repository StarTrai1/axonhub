package api

import (
	"context"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/server/orchestrator"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestResponsesWebSocketCloseSendsFirstCodeWithoutDataLock(t *testing.T) {
	for _, code := range []int{websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseInternalServerErr} {
		conn, client := heartbeatWebSocketPair(t)
		writer := &responsesWebSocketWriter{conn: conn}
		// A blocked data writer must not keep the session open indefinitely.
		writer.mu.Lock()
		writer.close(code)
		writer.close(websocket.ClosePolicyViolation)
		writer.mu.Unlock()
		require.NoError(t, client.SetReadDeadline(time.Now().Add(time.Second)))
		_, _, err := client.ReadMessage()
		require.True(t, websocket.IsCloseError(err, code), "expected close %d, got %v", code, err)
	}
}

func TestResponsesWebSocketHeartbeatCancellationSendsGoingAway(t *testing.T) {
	conn, client := heartbeatWebSocketPair(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	runResponsesWebSocketPings(ctx, &responsesWebSocketWriter{conn: conn}, make(chan struct{}), cancel, time.Hour)
	require.NoError(t, client.SetReadDeadline(time.Now().Add(time.Second)))
	_, _, err := client.ReadMessage()
	require.True(t, websocket.IsCloseError(err, websocket.CloseGoingAway), "got %v", err)
}

func TestResponsesWebSocketCloseOnProcessPanic(t *testing.T) {
	server := newResponsesWebSocketTestServer(t, func(context.Context, *httpclient.Request) (orchestrator.ChatCompletionResult, error) {
		panic("synthetic processing failure")
	}, nil)
	client := dialResponsesWebSocket(t, server.URL, nil)
	defer client.Close()
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"gpt-test","input":"hello"}`)))
	require.NoError(t, client.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, _, err := client.ReadMessage()
	require.True(t, websocket.IsCloseError(err, websocket.CloseInternalServerErr), "got %v", err)
}
