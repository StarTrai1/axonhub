package orchestrator

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestHTTP2FallbackPipeline(t *testing.T) {
	for _, scenario := range []struct {
		name, events string
		committed    bool
	}{
		{name: "metadata only"},
		{name: "text", committed: true, events: `{"type":"response.output_text.delta","item_id":"msg_partial","output_index":0,"content_index":0,"delta":"partial"}`},
		{name: "reasoning", committed: true, events: `{"type":"response.reasoning_summary_text.delta","item_id":"rs_partial","output_index":0,"summary_index":0,"delta":"partial reasoning"}`},
		{name: "custom tool", committed: true, events: `{"type":"response.output_item.added","output_index":0,"item":{"id":"ct_partial","type":"custom_tool_call","call_id":"custom_partial","name":"shell","input":""}}
{"type":"response.custom_tool_call_input.delta","item_id":"ct_partial","output_index":0,"delta":"pwd"}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var attempts atomic.Int32
			upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				id := "resp_http1"
				if r.ProtoMajor == 2 {
					id = "resp_reset"
				}
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":%q,\"object\":\"response\",\"model\":\"gpt-6.1-sol\",\"status\":\"in_progress\",\"output\":[]}}\n\n", id)
				if r.ProtoMajor == 2 {
					for _, event := range strings.Split(scenario.events, "\n") {
						if event != "" {
							_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
						}
					}
					w.(http.Flusher).Flush()
					panic(http.ErrAbortHandler)
				}
				_, _ = fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_ok\",\"output_index\":0,\"content_index\":0,\"delta\":\"recovered\"}\n\n")
				_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_http1\",\"object\":\"response\",\"model\":\"gpt-6.1-sol\",\"status\":\"completed\",\"output\":[]}}\n\n")
			}))
			upstream.EnableHTTP2 = true
			upstream.StartTLS()
			t.Cleanup(upstream.Close)
			roots := x509.NewCertPool()
			roots.AddCert(upstream.Certificate())
			client := httpclient.NewHttpClientWithProxy(&httpclient.ProxyConfig{Type: httpclient.ProxyTypeDisabled}, httpclient.WithTLSConfig(&tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}))
			t.Cleanup(client.CloseIdleConnections)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			process := func() (string, error) {
				out, err := responses.NewOutboundTransformer(upstream.URL, "synthetic-key")
				require.NoError(t, err)
				t.Cleanup(out.Stop)
				state := &PersistenceState{OriginalModel: "gpt-6.1-sol"}
				state.ChannelModelsCandidates = []*ChannelModelsCandidate{{
					Channel:     &biz.Channel{Channel: &ent.Channel{ID: 58, Name: "reset-fixture"}, HTTPClient: client, Outbound: out},
					APIFormat:   string(llm.APIFormatOpenAIResponse),
					TraceSticky: true,
					Models:      []biz.ChannelModelEntry{{RequestModel: "gpt-6.1-sol", ActualModel: "gpt-6.1-sol"}},
				}}
				p := pipeline.NewFactory(client).Pipeline(responses.NewInboundTransformer(), &PersistentOutboundTransformer{state: state}, pipeline.WithRetry(0, 1, 0))
				result, err := p.Process(ctx, &httpclient.Request{Method: http.MethodPost, URL: "/v1/responses", ContentType: "application/json", Body: []byte(`{"model":"gpt-6.1-sol","stream":true,"input":"synthetic request"}`)})
				if err != nil {
					return "", err
				}
				var data strings.Builder
				for result.EventStream.Next() {
					data.Write(result.EventStream.Current().Data)
				}
				err = result.EventStream.Err()
				require.NoError(t, result.EventStream.Close())
				return data.String(), err
			}
			data, err := process()
			if scenario.committed {
				// The Responses encoder emits one failed terminal event and consumes
				// the Go error so the handler does not append a second error event.
				require.NoError(t, err)
				require.Contains(t, data, "INTERNAL_ERROR; received from peer")
				require.Equal(t, 1, strings.Count(data, `"type":"response.failed"`))
				require.EqualValues(t, 1, attempts.Load(), "never replay committed text/reasoning/tool output")
				require.NotContains(t, data, "response.completed")
				data, err = process()
			}
			require.NoError(t, err)
			require.EqualValues(t, 2, attempts.Load())
			require.Contains(t, data, "recovered")
			require.NotContains(t, data, "resp_reset", "failed pre-output metadata must not escape")
			require.Contains(t, data, "response.completed")
		})
	}
}
