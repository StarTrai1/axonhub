package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/objects"
)

// Exercise the production orchestrator too: a transformer-only interrupt test
// misses the ownership record required before the next delta can reach it.
func TestResponsesProtocolInterruptedContinuation(t *testing.T) {
	for _, transport := range []string{"http", "ws"} {
		for _, cache := range []string{"custom", "omit"} {
			t.Run(transport+"/cache="+cache, func(t *testing.T) {
				var calls atomic.Int32
				respond := func(body []byte) string {
					n := calls.Add(1)
					require.Equal(t, cache == "custom", gjson.GetBytes(body, "prompt_cache_key").Exists())
					if cache == "custom" { require.Equal(t, "independent-cache", gjson.GetBytes(body, "prompt_cache_key").String()) }
					if n == 1 {
						return `{"type":"response.incomplete","response":{"id":"resp_interrupted","model":"synthetic-model","status":"incomplete","incomplete_details":{"reason":"interrupted"},"output":[],"usage":{"input_tokens":100,"output_tokens":17,"total_tokens":117,"input_tokens_details":{"cached_tokens":99}}}}`
					}
					require.Equal(t, int32(2), n)
					if transport == "ws" { require.Equal(t, "resp_interrupted", gjson.GetBytes(body, "previous_response_id").String()) }
					return `{"type":"response.completed","response":{"id":"resp_next","model":"synthetic-model","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"continued"}]}]}}`
				}
				upgrader := websocket.Upgrader{}
				upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if transport == "ws" {
						conn, err := upgrader.Upgrade(w, r, nil); require.NoError(t, err); defer conn.Close()
						for range 2 { _, body, err := conn.ReadMessage(); if err != nil { t.Error(err); return }; require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(respond(body)))) }
						return
					}
					body, err := readRequestBody(r); require.NoError(t, err)
					w.Header().Set("Content-Type", "text/event-stream")
					_, err = fmt.Fprintf(w, "data: %s\n\n", respond(body)); require.NoError(t, err)
				}))
				client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL, func(create *ent.ChannelCreate) {
					if transport == "ws" { create.SetEndpoints([]objects.ChannelEndpoint{{APIFormat: "openai/responses-ws"}}).SetSettings(&objects.ChannelSettings{ModelProtocols: []objects.ModelProtocol{{Model: "synthetic-model", APIFormats: []string{"openai/responses-ws"}}}}) }
				})
				gateway := responsesIntegrationGateway(t, client, handlers, project)
				conn := responsesIntegrationDial(t, gateway)
				for i := range 2 {
					payload := map[string]any{"type":"response.create", "model":"synthetic-model", "input":[]any{map[string]any{"role":"user", "content":"continue"}}, "client_metadata":map[string]string{"thread_id":"independent-thread"}}
					if cache == "custom" { payload["prompt_cache_key"] = "independent-cache" }
					if i == 1 { payload["previous_response_id"] = "resp_interrupted" }
					encoded, err := json.Marshal(payload); require.NoError(t, err)
					event := responsesIntegrationTurn(t, conn, string(encoded))
					if i == 0 { require.Equal(t, "response.incomplete", gjson.GetBytes(event, "type").String(), string(event)) } else { require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String(), string(event)) }
				}
				require.Equal(t, int32(2), calls.Load(), "the delta must not fail locally or force a full client replay")
				ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
				records, err := client.Request.Query().Order(ent.Asc(request.FieldID)).All(ctx); require.NoError(t, err); require.Len(t, records, 2)
				require.Equal(t, request.StatusCanceled, records[0].Status)
				require.Equal(t, request.StatusCompleted, records[1].Status)
			})
		}
	}
}
