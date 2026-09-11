package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/server/orchestrator"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestResponsesWebSocketWarmupRetainsInputAfterFailedContinuation(t *testing.T) {
	t.Parallel()

	for _, failure := range []string{"preflight", "response.failed", "response.canceled"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()

			requests := make(chan *httpclient.Request, 3)
			var attempts atomic.Int32
			process := func(_ context.Context, request *httpclient.Request) (orchestrator.ChatCompletionResult, error) {
				requests <- request
				if attempts.Add(1) == 1 {
					if failure == "preflight" {
						return orchestrator.ChatCompletionResult{}, &httpclient.Error{StatusCode: http.StatusBadGateway}
					}
					event := fmt.Sprintf(`{"type":%q,"response":{"id":"resp_failed","status":%q}}`, failure, strings.TrimPrefix(failure, "response."))
					return orchestrator.ChatCompletionResult{
						ChatCompletionStream: streams.SliceStream([]*httpclient.StreamEvent{{Type: failure, Data: []byte(event)}}),
					}, nil
				}
				return orchestrator.ChatCompletionResult{
					ChatCompletionStream: streams.SliceStream([]*httpclient.StreamEvent{{
						Type: "response.completed",
						Data: []byte(`{"type":"response.completed","response":{"id":"resp_retry","status":"completed"}}`),
					}}),
				}, nil
			}
			server := newResponsesWebSocketTestServer(t, process, nil)
			conn := dialResponsesWebSocket(t, server.URL, nil)
			defer conn.Close()

			warmup := `{"type":"response.create","model":"gpt-6-astra","generate":false,"instructions":"retain base","input":[{"type":"additional_tools","role":"developer","tools":[]},{"type":"message","role":"developer","content":"base"}]}`
			require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(warmup)))
			_, created, err := conn.ReadMessage()
			require.NoError(t, err)
			warmupID := gjson.GetBytes(created, "response.id").String()
			require.NotEmpty(t, warmupID)
			for range 2 {
				_, _, err := conn.ReadMessage()
				require.NoError(t, err)
			}

			continuation := fmt.Sprintf(`{"type":"response.create","model":"gpt-6-astra","previous_response_id":%q,"input":[{"type":"compaction","encrypted_content":"opaque-checkpoint"},{"type":"function_call_output","name":"automation_update","output":"heartbeat"}],"client_metadata":{"keep":"unchanged"}}`, warmupID)
			for attempt := range 2 {
				require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(continuation)))
				_, event, err := conn.ReadMessage()
				require.NoError(t, err)
				if attempt == 0 {
					require.NotEqual(t, "response.completed", gjson.GetBytes(event, "type").String())
				} else {
					require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String())
				}
			}

			first, retry := <-requests, <-requests
			require.JSONEq(t, string(first.Body), string(retry.Body))
			require.Equal(t, "retain base", gjson.GetBytes(retry.Body, "instructions").String())
			require.Len(t, gjson.GetBytes(retry.Body, "input").Array(), 4)
			require.Equal(t, "additional_tools", gjson.GetBytes(retry.Body, "input.0.type").String())
			require.Equal(t, "base", gjson.GetBytes(retry.Body, "input.1.content").String())
			require.Equal(t, "opaque-checkpoint", gjson.GetBytes(retry.Body, "input.2.encrypted_content").String())
			require.Equal(t, "automation_update", gjson.GetBytes(retry.Body, "input.3.name").String())
			require.Equal(t, "heartbeat", gjson.GetBytes(retry.Body, "input.3.output").String())
			require.False(t, gjson.GetBytes(retry.Body, "previous_response_id").Exists())
			require.Equal(t, "unchanged", gjson.GetBytes(retry.Body, "client_metadata.keep").String())

			// A completed continuation releases the warm-up; an expired ID is
			// subsequently passed to the normal history resolver without its prefix.
			require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(continuation)))
			_, _, err = conn.ReadMessage()
			require.NoError(t, err)
			afterCompletion := <-requests
			require.Equal(t, warmupID, gjson.GetBytes(afterCompletion.Body, "previous_response_id").String())
			require.Len(t, gjson.GetBytes(afterCompletion.Body, "input").Array(), 2)
			require.False(t, gjson.GetBytes(afterCompletion.Body, "instructions").Exists())
		})
	}
}
