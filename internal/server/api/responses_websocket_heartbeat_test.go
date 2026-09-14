package api

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func heartbeatWebSocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	connections := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := new(websocket.Upgrader)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err == nil {
			connections <- conn
		}
	}))
	t.Cleanup(server.Close)
	client := dialResponsesWebSocket(t, server.URL, nil)
	t.Cleanup(func() { _ = client.Close() })
	conn := <-connections
	t.Cleanup(func() { _ = conn.Close() })
	return conn, client
}

func TestResponsesWebSocketHeartbeatFailureCancelsSessionAndClosesConnection(t *testing.T) {
	conn, _ := heartbeatWebSocketPair(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	// A sent close frame makes subsequent ping writes fail without requiring
	// the peer to close its TCP connection or waiting for the idle timeout.
	require.NoError(t, conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second)))
	go runResponsesWebSocketPings(ctx, conn, make(chan struct{}), cancel, time.Millisecond)
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("heartbeat failure did not cancel the active session")
	}
	_, _, err := conn.ReadMessage()
	require.ErrorIs(t, err, net.ErrClosed)
}

func TestResponsesWebSocketHeartbeatStopKeepsHealthyConnection(t *testing.T) {
	conn, client := heartbeatWebSocketPair(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan struct{})
	close(done)
	runResponsesWebSocketPings(ctx, conn, done, cancel, time.Hour)
	require.NoError(t, ctx.Err())
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte("still open")))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	_, body, err := conn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "still open", string(body))
}
