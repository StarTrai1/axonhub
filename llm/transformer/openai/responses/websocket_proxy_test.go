package responses

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/auth"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestWebSocketProxyAliasesDialThroughSOCKS(t *testing.T) {
	for _, scheme := range []string{"socks", "socks5", "socks5h", "SOCKS"} {
		t.Run(scheme, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			defer listener.Close()
			done := make(chan error, 1)
			go func() {
				defer func() {
					if recovered := recover(); recovered != nil {
						t.Errorf("SOCKS fixture panic: %v", recovered)
					}
				}()
				conn, acceptErr := listener.Accept()
				if acceptErr != nil {
					done <- acceptErr
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				done <- serveSOCKSWebSocket(conn)
			}()

			client := httpclient.NewHttpClientWithProxy(&httpclient.ProxyConfig{
				Type: httpclient.ProxyTypeURL, URL: scheme + "://user:pass@" + listener.Addr().String(),
			})
			outbound, err := NewOutboundTransformerWithConfig(&Config{
				BaseURL: "http://unresolvable.invalid/v1", APIKeyProvider: auth.NewStaticKeyProvider("synthetic-key"), Transport: TransportWebSocket,
			})
			require.NoError(t, err)
			executor := outbound.CustomizeExecutor(client).(*WebSocketExecutor)
			ctx, cancel := context.WithTimeout(webSocketTestContext(), 5*time.Second)
			defer cancel()
			stream, err := executor.DoStream(ctx, &httpclient.Request{
				Method: http.MethodPost, URL: "http://unresolvable.invalid/v1/responses",
				Headers: http.Header{}, Body: []byte(`{"model":"gpt-6.1-sol","input":"hello","stream":true}`),
			})
			require.NoError(t, err)
			defer stream.Close()
			require.True(t, stream.Next())
			require.Equal(t, "response.completed", stream.Current().Type)
			require.NoError(t, <-done)
		})
	}
}

// Accept authenticated SOCKS5, then serve the tunneled WebSocket on the same
// socket. The invalid destination proves DNS is delegated to the proxy.
func serveSOCKSWebSocket(conn net.Conn) error {
	reader := bufio.NewReader(conn)
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return err
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return err
	}
	if _, err := conn.Write([]byte{5, 2}); err != nil {
		return err
	}
	if _, err := io.ReadFull(reader, header); err != nil {
		return err
	}
	username := make([]byte, int(header[1]))
	if _, err := io.ReadFull(reader, username); err != nil {
		return err
	}
	length, err := reader.ReadByte()
	if err != nil {
		return err
	}
	password := make([]byte, int(length))
	if _, err := io.ReadFull(reader, password); err != nil {
		return err
	}
	if string(username) != "user" || string(password) != "pass" {
		return errors.New("proxy authentication was not preserved")
	}
	if _, err := conn.Write([]byte{1, 0}); err != nil {
		return err
	}
	command := make([]byte, 5)
	if _, err := io.ReadFull(reader, command); err != nil {
		return err
	}
	if command[0] != 5 || command[1] != 1 || command[3] != 3 {
		return errors.New("expected SOCKS5 domain CONNECT")
	}
	destination := make([]byte, int(command[4])+2)
	if _, err := io.ReadFull(reader, destination); err != nil {
		return err
	}
	if string(destination[:len(destination)-2]) != "unresolvable.invalid" {
		return errors.New("destination hostname was not preserved")
	}
	if _, err := conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 80}); err != nil {
		return err
	}
	request, err := http.ReadRequest(reader)
	if err != nil {
		return err
	}
	writer := &proxyWebSocketWriter{ResponseRecorder: httptest.NewRecorder(), conn: conn, reader: reader}
	upgrader := websocket.Upgrader{}
	ws, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return err
	}
	defer ws.Close()
	var payload map[string]any
	if err := ws.ReadJSON(&payload); err != nil {
		return err
	}
	return ws.WriteJSON(map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_proxy", "status": "completed", "output": []any{}}})
}

type proxyWebSocketWriter struct {
	*httptest.ResponseRecorder
	conn   net.Conn
	reader *bufio.Reader
}

func (w *proxyWebSocketWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(w.reader, bufio.NewWriter(w.conn)), nil
}

func TestNormalizeWebSocketProxyPreservesSourceAndErrors(t *testing.T) {
	original, err := url.Parse("socks://user:pass@proxy.invalid:1080")
	require.NoError(t, err)
	got, err := normalizeWebSocketProxy(http.ProxyURL(original))(&http.Request{})
	require.NoError(t, err)
	require.Equal(t, "socks5", got.Scheme)
	require.Equal(t, "socks", original.Scheme)
	require.Equal(t, original.User, got.User)
	failure := errors.New("proxy resolution failed")
	_, err = normalizeWebSocketProxy(func(*http.Request) (*url.URL, error) { return nil, failure })(&http.Request{})
	require.ErrorIs(t, err, failure)
	require.Nil(t, normalizeWebSocketProxy(nil))
}

func TestWebSocketSOCKSCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		defer close(done)
		defer cancel()
		defer func() {
			if recovered := recover(); recovered != nil {
				t.Errorf("SOCKS cancellation fixture panic: %v", recovered)
			}
		}()
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		header := make([]byte, 2)
		if _, err := io.ReadFull(conn, header); err != nil {
			done <- err
			return
		}
		if _, err := io.CopyN(io.Discard, conn, int64(header[1])); err != nil {
			done <- err
			return
		}
		// Cancel while the proxy deliberately withholds its greeting reply.
		// Cancellation must close the connection without waiting for the full
		// WebSocket handshake timeout or opening a direct connection.
		cancel()
		_, err = conn.Read(header[:1])
		if err == nil {
			done <- errors.New("SOCKS connection continued after cancellation")
			return
		}
		if networkErr, ok := err.(net.Error); ok && networkErr.Timeout() {
			done <- err
			return
		}
		done <- nil
	}()
	executor := NewWebSocketExecutor(httpclient.NewHttpClientWithProxy(&httpclient.ProxyConfig{
		Type: httpclient.ProxyTypeURL, URL: "socks5://" + listener.Addr().String(),
	}))
	t.Cleanup(func() { _ = executor.Close() })
	_, err = executor.DoStream(ctx, &httpclient.Request{
		Method: http.MethodPost, URL: "https://unresolvable.invalid/v1/responses",
		Body: []byte(`{"model":"gpt-6.1-sol","input":"hello","stream":true}`),
	})
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, <-done)
}
