package responses

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestWebSocketExecutorInterruptSkipsTerminalSteeredResponse(t *testing.T) {
	for _, model := range []string{"gpt-6-sol", "gpt-6.1-sol"} {
		t.Run(model, func(t *testing.T) {
			upgrader := websocket.Upgrader{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				require.NoError(t, err)
				defer conn.Close()
				require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))
				var control map[string]any
				require.NoError(t, conn.ReadJSON(&control))
				require.Equal(t, model, control["model"])
				require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.created","response":{"id":"resp_old","status":"in_progress"}}`)))
				require.NoError(t, conn.ReadJSON(&control))
				require.Equal(t, "response.steer", control["type"])
				for _, event := range []string{
					`{"type":"response.steer.accepted","steer":{"id":"steer_1","previous_response_id":"resp_old"}}`,
					`{"type":"response.incomplete","response":{"id":"resp_old","status":"incomplete","incomplete_details":{"reason":"steered"}}}`,
					`{"type":"response.created","response":{"id":"resp_current","status":"in_progress"}}`,
				} {
					require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(event)))
				}
				control = nil
				require.NoError(t, conn.ReadJSON(&control))
				require.Equal(t, "response.interrupt", control["type"])
				require.Equal(t, "resp_current", control["response_id"], "stale interrupts must never reach the upstream socket")
				require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.incomplete","response":{"id":"resp_current","status":"incomplete","incomplete_details":{"reason":"interrupted"},"output":[]}}`)))
			}))
			defer server.Close()
			control := shared.NewResponsesWebSocketSteering(2)
			ctx, cancel := context.WithTimeout(shared.WithResponsesWebSocketSteer(webSocketTestContext(), control), 10*time.Second)
			defer cancel()
			executor := NewWebSocketExecutor(nil)
			stream, err := executor.DoStream(ctx, &httpclient.Request{
				Method: http.MethodPost,
				URL: server.URL + "/v1/responses",
				Body: []byte(fmt.Sprintf(`{"model":%q}`, model)),
			})
			require.NoError(t, err)
			defer stream.Close()
			require.True(t, stream.Next())
			require.Equal(t, "response.created", stream.Current().Type)
			require.True(t, control.Send([]byte(`{"type":"response.steer","previous_response_id":"resp_old","input":"changed"}`)))
			for _, eventType := range []string{"response.steer.accepted", "response.incomplete", "response.created"} {
				require.True(t, stream.Next())
				require.Equal(t, eventType, stream.Current().Type)
			}
			require.True(t, control.IsTerminalResponse("resp_old"))
			require.False(t, control.IsTerminalResponse("resp_current"))
			require.True(t, control.Send([]byte(`{"type":"response.interrupt","response_id":"resp_old","mode":"discard_partial_items"}`)))
			require.True(t, control.Send([]byte(`{"type":"response.interrupt","response_id":"resp_current","mode":"discard_partial_items"}`)))
			require.True(t, stream.Next())
			require.Equal(t, "response.incomplete", stream.Current().Type)
			require.True(t, control.IsTerminalResponse("resp_current"), "terminal ownership is visible before Next returns")
			require.False(t, stream.Next())
			require.NoError(t, stream.Err())
		})
	}
}
