package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/request"
)

// This reproduces agent's HTTP/SSE upstream behind the multiplexed downstream
// socket, including durable history, final delivery and a subsequent delta.
func TestResponsesProtocolHTTPInterruptIsolationAndContinuation(t *testing.T) {
	for _, kind := range []channel.Type{channel.TypeOpenaiResponses, channel.TypeCodex} {
		for _, cache := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cache=%t", kind, cache), func(t *testing.T) {
				var calls atomic.Int32
				cancelled := make(chan struct{})
				releaseOther := make(chan struct{})
				upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					body, err := readRequestBody(r)
					require.NoError(t, err)
					input := gjson.GetBytes(body, "input").Raw
					w.Header().Set("Content-Type", "text/event-stream")
					write := func(event string) {
						_, err := fmt.Fprintf(w, "data: %s\n\n", event)
						require.NoError(t, err)
						w.(http.Flusher).Flush()
					}
					if strings.Contains(input, `"other"`) {
						write(`{"type":"response.created","response":{"id":"resp_other","model":"synthetic-model","status":"in_progress","output":[]}}`)
						write(`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_other","role":"assistant","status":"in_progress","content":[]}}`)
						write(`{"type":"response.output_text.delta","item_id":"msg_other","output_index":0,"content_index":0,"delta":"Other lane active."}`)
						select {
						case <-releaseOther:
						case <-r.Context().Done():
							t.Error("interrupt cancelled another lane")
							return
						}
						write(`{"type":"response.completed","response":{"id":"resp_other","model":"synthetic-model","status":"completed","output":[]}}`)
						return
					}
					require.Equal(t, cache, gjson.GetBytes(body, "prompt_cache_key").Exists())
					if cache {
						require.Equal(t, "original-cache", gjson.GetBytes(body, "prompt_cache_key").String())
					}
					if strings.Contains(input, `"continue"`) {
						require.False(t, gjson.GetBytes(body, "previous_response_id").Exists(), "cancelled HTTP state must be restored locally")
						require.Contains(t, input, "opaque-kept-ciphertext")
						require.Contains(t, input, "Completed result.")
						require.Contains(t, input, "first")
						require.NotContains(t, input, "rs_partial")
						write(`{"type":"response.completed","response":{"id":"resp_next","model":"synthetic-model","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"continued"}]}]}}`)
						return
					}
					for _, event := range []string{
						`{"type":"response.created","response":{"id":"resp_stopped","model":"synthetic-model","status":"in_progress","output":[]}}`,
						`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_kept","encrypted_content":"opaque-kept-ciphertext","summary":[]}}`,
						`{"type":"response.output_item.done","output_index":1,"item":{"type":"message","id":"msg_kept","role":"assistant","status":"completed","phase":"partial_answer","content":[{"type":"output_text","text":"Completed result."}]}}`,
						`{"type":"response.output_item.added","output_index":2,"item":{"type":"reasoning","id":"rs_partial","summary":[]}}`,
					} {
						write(event)
					}
					<-r.Context().Done()
					close(cancelled)
				}))
				client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL, func(c *ent.ChannelCreate) { c.SetType(kind) })
				gateway := responsesIntegrationGateway(t, client, handlers, project)
				conn := responsesIntegrationDial(t, gateway)
				require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))
				cacheField := ""
				if cache {
					cacheField = `,"prompt_cache_key":"original-cache"`
				}
				require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","stream_id":"stop","model":"synthetic-model","input":[{"role":"user","content":"first"}],"client_metadata":{"thread_id":"stop-thread"}`+cacheField+`}`)))
				require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","stream_id":"other","model":"synthetic-model","input":[{"role":"user","content":"other"}],"client_metadata":{"thread_id":"other-thread"}}`)))
				ready, otherReady := false, false
				for !ready || !otherReady {
					_, event, err := conn.ReadMessage()
					require.NoError(t, err)
					require.NotEqual(t, "error", gjson.GetBytes(event, "type").String(), string(event))
					ready = ready || gjson.GetBytes(event, "item.id").String() == "rs_partial"
					otherReady = otherReady || gjson.GetBytes(event, "response.id").String() == "resp_other"
				}
				require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.interrupt","response_id":"resp_stopped","mode":"discard_partial_items"}`)))
				accepted, discarded := false, false
				for {
					_, event, err := conn.ReadMessage()
					require.NoError(t, err)
					typ := gjson.GetBytes(event, "type").String()
					require.NotEqual(t, "error", typ, string(event))
					accepted = accepted || typ == "response.interrupt.accepted"
					discarded = discarded || typ == "response.output_item.interrupted"
					if typ == "response.incomplete" {
						require.Equal(t, "stop", gjson.GetBytes(event, "stream_id").String())
						require.Equal(t, "interrupted", gjson.GetBytes(event, "response.incomplete_details.reason").String())
						require.Len(t, gjson.GetBytes(event, "response.output").Array(), 2)
						require.Contains(t, string(event), "opaque-kept-ciphertext")
						require.NotContains(t, string(event), "rs_partial")
						break
					}
				}
				require.True(t, accepted)
				require.True(t, discarded)
				responsesIntegrationAwait(t, cancelled)
				close(releaseOther)
				for {
					_, event, err := conn.ReadMessage()
					require.NoError(t, err)
					if gjson.GetBytes(event, "type").String() == "response.completed" {
						require.Equal(t, "other", gjson.GetBytes(event, "stream_id").String())
						break
					}
				}
				event := responsesIntegrationTurn(t, conn, `{"type":"response.create","stream_id":"stop","model":"synthetic-model","previous_response_id":"resp_stopped","input":[{"role":"user","content":"continue"}],"client_metadata":{"thread_id":"stop-thread"}`+cacheField+`}`)
				require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String(), string(event))
				require.Equal(t, int32(3), calls.Load(), "interruption must not regenerate the cancelled answer")
				ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
				require.Eventually(t, func() bool {
					count, err := client.Request.Query().Where(request.StatusEQ(request.StatusCanceled)).Count(ctx)
					return err == nil && count == 1
				}, 2*time.Second, 10*time.Millisecond)
			})
		}
	}
}
