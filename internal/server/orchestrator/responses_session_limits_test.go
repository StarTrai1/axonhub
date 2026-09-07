package orchestrator

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestResponsesSessionPublishesLargeHistoryBeforePersistence(t *testing.T) {
	ctx := shared.WithSessionScope(context.Background(), "large-owner")
	store := newResponsesSessionStore(func(context.Context, string) ([]byte, []byte, bool, error) {
		t.Fatal("a just-completed large history must not depend on database persistence")
		return nil, nil, false, nil
	})
	history := strings.Repeat("x", responsesSessionMaxResponse+4096)
	requestBody := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"` + history + `"}]}`)
	terminal := &httpclient.StreamEvent{Data: []byte(`{"type":"response.completed","response":{"id":"resp_large_live","status":"completed","output":[{"type":"function_call","id":"fc_native","call_id":"call_native","name":"exec","arguments":"{}"}]}}`)}
	wrapped := store.wrapStream(ctx, requestBody, streams.SliceStream([]*httpclient.StreamEvent{terminal}))
	require.True(t, wrapped.Next())
	prepared, _ := store.prepare(ctx, []byte(`{"previous_response_id":"resp_large_live","input":[{"type":"function_call_output","call_id":"call_native","output":"ok"}]}`))
	require.False(t, gjson.GetBytes(prepared, "previous_response_id").Exists())
	require.Equal(t, history, gjson.GetBytes(prepared, "input.0.content").String())
	require.Equal(t, "fc_native", gjson.GetBytes(prepared, "input.1.id").String())
	require.Equal(t, int64(3), gjson.GetBytes(prepared, "input.#").Int())
	require.LessOrEqual(t, store.totalBytes, responsesSessionMaxBytes)
	require.NoError(t, wrapped.Close())
}

func TestResponsesSessionRetainsTerminalStateAfterWireBufferOverflow(t *testing.T) {
	for _, fullTerminal := range []bool{false, true} {
		name := "item_done_snapshot"
		if fullTerminal {
			name = "full_terminal_snapshot"
		}
		t.Run(name, func(t *testing.T) {
			ctx := shared.WithSessionScope(context.Background(), "owner")
			store := newResponsesSessionStore()
			item := json.RawMessage(`{"type":"custom_tool_call","id":"ctc_native","call_id":"call_native","name":"exec","input":"complete input","async":true}`)
			itemEvent, err := json.Marshal(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": item})
			require.NoError(t, err)
			output := []json.RawMessage{}
			if fullTerminal {
				output = append(output, item)
			}
			terminal, err := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": "resp_overflow", "status": "completed", "output": output}})
			require.NoError(t, err)
			padding := []byte(`{"type":"response.in_progress","padding":"` + strings.Repeat("x", responsesSessionMaxResponse) + `"}`)
			events := []*httpclient.StreamEvent{{Data: padding}, {Data: itemEvent}, {Data: terminal}}
			wrapped := store.wrapStream(ctx, []byte(`{"input":[{"role":"user","content":"original"}]}`), streams.SliceStream(events))
			for range events {
				require.True(t, wrapped.Next())
			}
			require.True(t, wrapped.(*responsesSessionStream).overflow)
			prepared, _ := store.prepare(ctx, []byte(`{"previous_response_id":"resp_overflow","input":[{"type":"custom_tool_call_output","call_id":"call_native","output":"ok"}]}`))
			require.False(t, gjson.GetBytes(prepared, "previous_response_id").Exists())
			require.Equal(t, "original", gjson.GetBytes(prepared, "input.0.content").String())
			require.Equal(t, "complete input", gjson.GetBytes(prepared, "input.1.input").String())
			require.True(t, gjson.GetBytes(prepared, "input.1.async").Bool())
			require.NoError(t, wrapped.Close())
		})
	}
}

func TestResponsesSessionRejectsRecordsAboveReplayLimit(t *testing.T) {
	ctx := shared.WithSessionScope(context.Background(), "owner")
	store := newResponsesSessionStore()
	body := []byte(`{"input":"` + strings.Repeat("x", responsesSessionMaxReplay) + `"}`)
	store.record(ctx, body, []byte(`{"id":"resp_oversized","status":"completed","output":[]}`))
	require.Nil(t, store.lookup(ctx, "resp_oversized"))
	require.Zero(t, store.totalBytes)
}

func TestResponsesSessionDoesNotCacheErroredCompletion(t *testing.T) {
	for _, terminal := range []string{
		`{"type":"response.completed","response":{"id":"resp_invalid","status":"completed","error":{"code":"server_error"},"output":[{"type":"message","role":"assistant","content":"partial"}]}}`,
		`{"type":"response.completed","response":{"id":"resp_invalid","status":"failed","output":[]}}`,
	} {
		ctx := shared.WithSessionScope(context.Background(), "owner")
		store := newResponsesSessionStore()
		events := []*httpclient.StreamEvent{
			{Data: []byte(`{"type":"response.created","response":{"id":"resp_invalid","status":"in_progress","output":[]}}`)},
			{Data: []byte(terminal)},
		}
		wrapped := store.wrapStream(ctx, []byte(`{"input":"history"}`), streams.SliceStream(events))
		_, err := streams.All(wrapped)
		require.NoError(t, err)
		require.Nil(t, store.lookup(ctx, "resp_invalid"))
		require.NoError(t, wrapped.Close())
	}
}

func TestResponsesSessionRetainsDoneItemsMissingFromTerminal(t *testing.T) {
	ctx := shared.WithSessionScope(context.Background(), "owner")
	store := newResponsesSessionStore()
	events := []*httpclient.StreamEvent{
		{Data: []byte(`{"type":"response.in_progress","padding":"` + strings.Repeat("x", responsesSessionMaxResponse) + `"}`)},
		{Data: []byte(`{"type":"response.output_item.done","output_index":1,"item":{"type":"custom_tool_call","id":"ctc_native","call_id":"call_native","name":"exec","input":"complete","async":true}}`)},
		{Data: []byte(`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","role":"assistant","content":"answer"}}`)},
		{Data: []byte(`{"type":"response.completed","response":{"id":"resp_partial","status":"completed","output":[{"type":"message","role":"assistant","content":"answer"}]}}`)},
	}
	wrapped := store.wrapStream(ctx, []byte(`{"input":"original"}`), streams.SliceStream(events))
	_, err := streams.All(wrapped)
	require.NoError(t, err)
	prepared, _ := store.prepare(ctx, []byte(`{"previous_response_id":"resp_partial","input":[{"type":"custom_tool_call_output","call_id":"call_native","output":"done"}]}`))
	require.False(t, gjson.GetBytes(prepared, "previous_response_id").Exists())
	require.Equal(t, int64(4), gjson.GetBytes(prepared, "input.#").Int())
	require.Equal(t, "complete", gjson.GetBytes(prepared, "input.2.input").String())
	require.True(t, gjson.GetBytes(prepared, "input.2.async").Bool())
	require.NoError(t, wrapped.Close())
}

func TestResponsesSessionRejectsUnexpandedLiveDelta(t *testing.T) {
	ctx := shared.WithSessionScope(context.Background(), "owner")
	store := newResponsesSessionStore()
	store.record(ctx, []byte(`{"previous_response_id":"resp_ancestor","input":[]}`), []byte(`{"id":"resp_delta","status":"completed","output":[]}`))
	require.Nil(t, store.lookup(ctx, "resp_delta"))
}
