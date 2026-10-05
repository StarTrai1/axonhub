package orchestrator

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/auth"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestWebSocketSOCKSChannelRetry(t *testing.T) {
	for _, tc := range []struct {
		name        string
		scheme      string
		failure     string
		connections int32
		creates     int32
		wantError   bool
	}{
		{"socks handshake reset", "socks", "socks", 2, 1, false},
		{"WSS upgrade reset", "socks5", "upgrade", 2, 1, false},
		{"SOCKS5H WSS", "socks5h", "", 1, 1, false},
		{"proxy auth rejection", "socks5", "auth", 1, 0, true},
		{"reset after delivered output", "socks5", "output", 1, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			certServer := httptest.NewTLSServer(nil)
			t.Cleanup(certServer.Close)
			fixture := newSOCKSRetryFixture(t, certServer.TLS, tc.failure)
			client := httpclient.NewHttpClientWithProxy(&httpclient.ProxyConfig{
				Type: httpclient.ProxyTypeURL, URL: tc.scheme + "://" + fixture.listener.Addr().String(),
				Username: "fixture-user", Password: "fixture-pass",
			})
			roots := x509.NewCertPool()
			roots.AddCert(certServer.Certificate())
			// Trust only the local fixture certificate; production TLS verification
			// remains enabled even though SOCKS resolves a synthetic hostname.
			client.GetNativeClient().Transport.(*http.Transport).TLSClientConfig = &tls.Config{
				RootCAs: roots, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12,
			}
			out, err := responses.NewOutboundTransformerWithConfig(&responses.Config{
				BaseURL: "https://unresolvable.invalid/v1", Transport: responses.TransportWebSocket,
				APIKeyProvider: auth.NewStaticKeyProvider("synthetic-api-key"),
			})
			require.NoError(t, err)
			t.Cleanup(out.Stop)
			state := &PersistenceState{OriginalModel: "gpt-6.1-sol"}
			state.ChannelModelsCandidates = []*ChannelModelsCandidate{{
				Channel: &biz.Channel{
					Channel: &ent.Channel{Name: "official-key-fixture"}, HTTPClient: client, Outbound: out,
				},
				APIFormat: string(llm.APIFormatOpenAIResponse),
				Models:    []biz.ChannelModelEntry{{RequestModel: "gpt-6.1-sol", ActualModel: "gpt-6.1-sol"}},
			}}
			outbound := &PersistentOutboundTransformer{state: state}
			p := pipeline.NewFactory(httpclient.NewHttpClientWithProxy(&httpclient.ProxyConfig{
				Type: httpclient.ProxyTypeDisabled,
			})).Pipeline(openai.NewInboundTransformer(), outbound, pipeline.WithRetry(0, 1, 0))
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			result, processErr := p.Process(ctx, buildTestRequest("gpt-6.1-sol", "hello", true))
			var streamErr error
			var delivered strings.Builder
			if processErr == nil {
				for result.EventStream.Next() {
					event := result.EventStream.Current()
					if event == nil {
						continue
					}
					delivered.Write(event.Data)
					if strings.Contains(string(event.Data), "partial") {
						select {
						case fixture.outputDelivered <- struct{}{}:
						default:
						}
					}
				}
				streamErr = result.EventStream.Err()
				require.NoError(t, result.EventStream.Close())
			}
			if tc.wantError {
				require.Error(t, errors.Join(processErr, streamErr))
				if tc.failure == "output" {
					require.Contains(t, delivered.String(), "partial")
				}
			} else {
				require.NoError(t, processErr)
				require.NoError(t, streamErr)
				require.Equal(t, 1, strings.Count(delivered.String(), "recovered"))
			}
			require.Equal(t, tc.connections, fixture.connections.Load())
			require.Equal(t, tc.creates, fixture.creates.Load(), "handshake retries must not duplicate response.create")
		})
	}
}

type socksRetryFixture struct {
	listener        net.Listener
	tlsConfig       *tls.Config
	failure         string
	connections     atomic.Int32
	creates         atomic.Int32
	outputDelivered chan struct{}
}

func newSOCKSRetryFixture(t *testing.T, tlsConfig *tls.Config, failure string) *socksRetryFixture {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	fixture := &socksRetryFixture{
		listener: listener, tlsConfig: tlsConfig, failure: failure, outputDelivered: make(chan struct{}, 1),
	}
	done := make(chan error, 1)
	go func() {
		defer close(done)
		defer func() {
			if recovered := recover(); recovered != nil {
				done <- fmt.Errorf("SOCKS fixture panic: %v", recovered)
			}
		}()
		for {
			conn, err := listener.Accept()
			if errors.Is(err, net.ErrClosed) {
				return
			}
			if err != nil {
				done <- err
				return
			}
			attempt := fixture.connections.Add(1)
			if err := fixture.serve(conn, attempt); err != nil {
				done <- err
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(12 * time.Second):
			t.Error("SOCKS fixture did not stop")
		}
	})
	return fixture
}

func (f *socksRetryFixture) serve(conn net.Conn, attempt int32) error {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	reader := bufio.NewReader(conn)
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return err
	}
	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return err
	}
	if attempt == 1 && f.failure == "socks" {
		return conn.(*net.TCPConn).SetLinger(0)
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
	if string(username) != "fixture-user" || string(password) != "fixture-pass" {
		return errors.New("SOCKS credentials were not preserved")
	}
	if f.failure == "auth" {
		_, err := conn.Write([]byte{1, 1})
		return err
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
	if string(destination[:len(destination)-2]) != "unresolvable.invalid" || destination[len(destination)-2] != 1 || destination[len(destination)-1] != 187 {
		return errors.New("WSS destination must remain unresolvable.invalid:443 through SOCKS")
	}
	if _, err := conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 1, 187}); err != nil {
		return err
	}
	tlsConn := tls.Server(conn, f.tlsConfig)
	reader = bufio.NewReader(tlsConn)
	request, err := http.ReadRequest(reader)
	if err != nil {
		return err
	}
	if request.URL.Path != "/v1/responses" || request.Header.Get("Authorization") != "Bearer synthetic-api-key" {
		return errors.New("API-key WSS handshake lost its route or authorization")
	}
	if attempt == 1 && f.failure == "upgrade" {
		return conn.(*net.TCPConn).SetLinger(0)
	}
	writer := &socksRetryResponseWriter{ResponseRecorder: httptest.NewRecorder(), conn: tlsConn, reader: reader}
	upgrader := websocket.Upgrader{}
	ws, err := upgrader.Upgrade(writer, request, nil)
	if err != nil {
		return err
	}
	var payload struct {
		Type  string `json:"type"`
		Model string `json:"model"`
	}
	if err := ws.ReadJSON(&payload); err != nil {
		return err
	}
	if payload.Type != "response.create" || payload.Model != "gpt-6.1-sol" {
		return errors.New("unexpected Responses request payload")
	}
	f.creates.Add(1)
	text := "recovered"
	if f.failure == "output" {
		text = "partial"
	}
	if err := ws.WriteJSON(map[string]any{"type": "response.output_text.delta", "item_id": "msg_fixture", "output_index": 0, "content_index": 0, "delta": text}); err != nil {
		return err
	}
	if f.failure == "output" {
		select {
		case <-f.outputDelivered:
			return conn.(*net.TCPConn).SetLinger(0)
		case <-time.After(10 * time.Second):
			return errors.New("client did not receive partial output")
		}
	}
	return ws.WriteJSON(map[string]any{"type": "response.completed", "response": map[string]any{
		"id": "resp_fixture", "status": "completed", "model": "gpt-6.1-sol", "output": []any{},
	}})
}

type socksRetryResponseWriter struct {
	*httptest.ResponseRecorder

	conn   net.Conn
	reader *bufio.Reader
}

func (w *socksRetryResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.conn, bufio.NewReadWriter(w.reader, bufio.NewWriter(w.conn)), nil
}
