package responses

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestCodex162IncrementalHistoryPreservesOrder(t *testing.T) {
	// 0.162 sends base instructions in input and keeps multiple catalog deltas
	// with intervening calls, partial answers and removal notices in history.
	input := `[
		{"type":"additional_tools","id":"at_first","role":"developer","tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]}]},
		{"type":"message","id":"msg_base","role":"developer","content":[{"type":"input_text","text":"Follow the project instructions."}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"Inspect the project."}]},
		{"type":"message","id":"msg_partial","role":"assistant","phase":"partial_answer","content":[{"type":"output_text","text":"First result."}]},
		{"type":"function_call","id":"fc_first","call_id":"call_first","name":"lookup","namespace":"functions","arguments":"{}"},
		{"type":"function_call_output","call_id":"call_first","output":"found"},
		{"type":"additional_tools","id":"at_update","role":"developer","tools":[{"type":"namespace","name":"functions","description":"This is an incremental namespace update. Previously declared tools remain available for direct calls unless explicitly marked unavailable.","tools":[{"type":"function","name":"inspect","parameters":{"type":"object"}}]}]},
		{"type":"message","role":"developer","content":[{"type":"input_text","text":"The following tools are no longer available. Do not call them:\n- functions.lookup"}]},
		{"type":"reasoning","id":"rs_retained","encrypted_content":"opaque-retained","summary":[]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"Continue."}]}
	]`
	request := &httpclient.Request{Body: []byte(`{"model":"gpt-6.1-sol","stream":true,"input":`+input+`}`)}
	before := string(request.Body)
	unified, err := NewInboundTransformer().TransformRequest(t.Context(), request)
	require.NoError(t, err)
	outbound, err := NewOutboundTransformer("https://api.example/v1", "synthetic-key")
	require.NoError(t, err)
	wire, err := outbound.TransformRequest(t.Context(), unified)
	require.NoError(t, err)
	got := gjson.GetBytes(wire.Body, "input").Array()
	want := gjson.Parse(input).Array()
	require.Len(t, got, len(want))
	for i := range want {
		require.Equal(t, want[i].Get("type").String(), got[i].Get("type").String(), "item %d", i)
	}
	for _, i := range []int{0, 6} {
		require.JSONEq(t, want[i].Raw, got[i].Raw)
	}
	require.Equal(t, "developer", got[1].Get("role").String())
	require.Equal(t, "Follow the project instructions.", got[1].Get("content.0.text").String())
	require.Empty(t, gjson.GetBytes(wire.Body, "instructions").String())
	require.Equal(t, "partial_answer", got[3].Get("phase").String())
	require.Equal(t, "call_first", got[4].Get("call_id").String())
	require.Equal(t, "call_first", got[5].Get("call_id").String())
	require.Equal(t, want[7].Get("content.0.text").String(), got[7].Get("content.0.text").String())
	require.Equal(t, "opaque-retained", got[8].Get("encrypted_content").String())
	require.Equal(t, before, string(request.Body))
}

func TestCodex162PartialAnswerDoesNotEndTurn(t *testing.T) {
	item := `{"type":"message","id":"msg_partial","role":"assistant","phase":"partial_answer","status":"completed","content":[{"type":"output_text","text":"First result."}]}`
	terminal := `{"id":"resp_partial","object":"response","model":"gpt-6.1-sol","status":"completed","end_turn":false,"output":[`+item+`]}`
	outbound, err := NewOutboundTransformer("https://api.example/v1", "synthetic-key")
	require.NoError(t, err)
	unified, err := outbound.TransformResponse(t.Context(), &httpclient.Response{StatusCode: 200, Body: []byte(terminal)})
	require.NoError(t, err)
	serialized, err := json.Marshal(unified)
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(serialized, "end_turn").Exists())
	wire, err := NewInboundTransformer().TransformResponse(t.Context(), unified)
	require.NoError(t, err)
	require.Equal(t, "partial_answer", gjson.GetBytes(wire.Body, "output.0.phase").String())
	require.Equal(t, "false", gjson.GetBytes(wire.Body, "end_turn").Raw)

	events := []*httpclient.StreamEvent{
		{Type: "response.created", Data: []byte(`{"type":"response.created","response":{"id":"resp_partial","model":"gpt-6.1-sol","status":"in_progress","output":[]}}`)},
		{Type: "response.output_item.added", Data: []byte(`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg_partial","role":"assistant","phase":"partial_answer","content":[]}}`)},
		{Type: "response.output_text.delta", Data: []byte(`{"type":"response.output_text.delta","item_id":"msg_partial","output_index":0,"content_index":0,"delta":"First result."}`)},
		{Type: "response.output_item.done", Data: []byte(`{"type":"response.output_item.done","output_index":0,"item":`+item+`}`)},
		{Type: "response.completed", Data: []byte(`{"type":"response.completed","response":`+terminal+`}`)},
	}
	converted, err := outbound.TransformStream(t.Context(), &httpclient.Request{}, streams.SliceStream(events))
	require.NoError(t, err)
	client, err := NewInboundTransformer().TransformStream(t.Context(), converted)
	require.NoError(t, err)
	chunks, err := streams.All(client)
	require.NoError(t, err)
	require.NoError(t, client.Close())
	seen := map[StreamEventType]bool{}
	for _, chunk := range chunks {
		var event StreamEvent
		require.NoError(t, json.Unmarshal(chunk.Data, &event))
		if event.Item != nil && event.Item.Type == "message" {
			require.NotNil(t, event.Item.Phase)
			require.Equal(t, "partial_answer", *event.Item.Phase)
			seen[event.Type] = true
		}
		if event.Type == StreamEventTypeResponseCompleted {
			require.Equal(t, "false", gjson.GetBytes(chunk.Data, "response.end_turn").Raw)
		}
	}
	require.True(t, seen[StreamEventTypeOutputItemAdded])
	require.True(t, seen[StreamEventTypeOutputItemDone])
	for _, source := range [][]*httpclient.StreamEvent{events, chunks} {
		body, _, err := AggregateStreamChunks(t.Context(), source)
		require.NoError(t, err)
		require.Equal(t, "partial_answer", gjson.GetBytes(body, "output.0.phase").String())
		require.Equal(t, "false", gjson.GetBytes(body, "end_turn").Raw)
	}
}
