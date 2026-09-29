package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/server/orchestrator"
	"github.com/looplj/axonhub/llm/auth"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestResponsesWebSocketInterruptDrainsAndContinues(t *testing.T) {
	var connections atomic.Int32
	upgrader := websocket.Upgrader{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections.Add(1)
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer conn.Close()
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(10 * time.Second)))
		var create map[string]any
		require.NoError(t, conn.ReadJSON(&create))
		require.Equal(t, "response.create", create["type"])
		for _, event := range []string{
			`{"type":"response.created","response":{"id":"resp_interrupt","model":"gpt-6-sol","status":"in_progress","output":[]}}`,
			`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"rs_discard","summary":[]}}`,
			`{"type":"response.reasoning_summary_text.delta","item_id":"rs_discard","output_index":0,"summary_index":0,"delta":"thinking"}`,
		} {
			require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(event)))
		}
		var interrupt json.RawMessage
		require.NoError(t, conn.ReadJSON(&interrupt))
		require.JSONEq(t, `{"type":"response.interrupt","response_id":"resp_interrupt","mode":"discard_partial_items"}`, string(interrupt))
		for _, event := range []string{
			`{"type":"response.interrupt.accepted","response_id":"resp_interrupt"}`,
			`{"type":"response.output_item.interrupted","response_id":"resp_interrupt","item_id":"rs_discard","output_index":0}`,
			`{"type":"response.incomplete","response":{"id":"resp_interrupt","model":"gpt-6-sol","status":"incomplete","incomplete_details":{"reason":"interrupted"},"output":[],"usage":{"input_tokens":10,"output_tokens":3,"total_tokens":13}}}`,
		} {
			require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(event)))
		}
		require.NoError(t, conn.ReadJSON(&create))
		require.Equal(t, "resp_interrupt", create["previous_response_id"])
		require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"resp_after","model":"gpt-6-sol","status":"completed","output":[]}}`)))
	}))
	defer upstream.Close()
	outbound, err := responses.NewOutboundTransformerWithConfig(&responses.Config{
		BaseURL: upstream.URL, APIKeyProvider: auth.NewStaticKeyProvider("synthetic"), Transport: responses.TransportWebSocket,
	})
	require.NoError(t, err)
	defer outbound.Stop()
	p := pipeline.NewFactory(httpclient.NewHttpClient()).Pipeline(responses.NewInboundTransformer(), outbound)
	process := func(ctx context.Context, request *httpclient.Request) (orchestrator.ChatCompletionResult, error) {
		ctx = shared.WithSessionScope(ctx, "interrupt-test")
		result, err := p.Process(ctx, request)
		if err != nil {
			return orchestrator.ChatCompletionResult{}, err
		}
		return orchestrator.ChatCompletionResult{ChatCompletionStream: result.EventStream}, nil
	}
	server := newResponsesWebSocketTestServer(t, process, nil)
	conn := dialResponsesWebSocket(t, server.URL, nil)
	defer conn.Close()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(10 * time.Second)))
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"gpt-6-sol","input":"first"}`)))
	_, created, err := conn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, "resp_interrupt", gjson.GetBytes(created, "response.id").String())
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.interrupt","response_id":"resp_interrupt","mode":"discard_partial_items"}`)))
	seen := map[string]bool{}
	for {
		_, event, err := conn.ReadMessage()
		require.NoError(t, err)
		typ := gjson.GetBytes(event, "type").String()
		seen[typ] = true
		require.NotEqual(t, "error", typ, string(event))
		if typ == "response.incomplete" {
			require.Equal(t, "interrupted", gjson.GetBytes(event, "response.incomplete_details.reason").String())
			require.Empty(t, gjson.GetBytes(event, "response.output").Array())
			require.Equal(t, int64(13), gjson.GetBytes(event, "response.usage.total_tokens").Int())
			break
		}
	}
	require.True(t, seen["response.interrupt.accepted"])
	require.True(t, seen["response.output_item.interrupted"])
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"gpt-6-sol","previous_response_id":"resp_interrupt","input":"second"}`)))
	for {
		_, event, err := conn.ReadMessage()
		require.NoError(t, err)
		typ := gjson.GetBytes(event, "type").String()
		require.NotEqual(t, "error", typ, string(event))
		if typ == "response.completed" {
			break
		}
	}
	require.Equal(t, int32(1), connections.Load())
}

func TestResponsesWebSocketInterruptOwnershipAndValidation(t *testing.T) {
	d := &responsesWebSocketDispatcher{responseLanes: make(map[string]*responsesWebSocketLane)}
	for _, payload := range []string{
		`{"type":"response.interrupt","response_id":"foreign","mode":"discard_partial_items"}`,
		`{"type":"response.interrupt","response_id":"foreign","mode":"unknown"}`,
		`{"type":"response.interrupt","response_id":"foreign","mode":"discard_partial_items","stream_id":"other"}`,
		`{"type":"response.interrupt","mode":"discard_partial_items"}`,
	} {
		require.NotNil(t, d.routeInterrupt([]byte(payload)))
	}
	lane := &responsesWebSocketLane{}
	control := lane.begin("gpt-6-luna")
	d.registerResponseID("owned", lane)
	interrupt := []byte(`{"type":"response.interrupt","response_id":"owned","mode":"discard_partial_items"}`)
	require.NotNil(t, d.routeInterrupt(interrupt), "HTTP upstream cannot claim a graceful interrupt")
	control.Activate()
	require.Nil(t, d.routeInterrupt(interrupt))
	require.JSONEq(t, string(interrupt), string(<-control.Events()))
	lane.end()
	require.NotNil(t, d.routeInterrupt(interrupt))
	d.unregisterLane(lane)
	require.NotNil(t, d.routeInterrupt(interrupt))
}
