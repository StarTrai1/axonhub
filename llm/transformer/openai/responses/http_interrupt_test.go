package responses

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func httpInterruptFixture(t *testing.T, ctx context.Context, raw ...string) (*httpInterruptStream, *shared.ResponsesWebSocketSteering) {
	t.Helper()
	events := make([]*httpclient.StreamEvent, 0, len(raw))
	for _, body := range raw {
		events = append(events, &httpclient.StreamEvent{Type: gjson.Get(body, "type").String(), Data: []byte(body)})
	}
	control := shared.NewResponsesWebSocketSteering(1)
	_, cancel := context.WithCancel(ctx)
	s := &httpInterruptStream{ctx: ctx, cancel: cancel, source: streams.SliceStream(events), control: control, completed: make(map[int]json.RawMessage), partial: make(map[int]string)}
	s.unregister = control.RegisterHTTPInterrupt(s.interrupt)
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	return s, control
}

func TestHTTPInterruptBeforeCompletionDiscardsUnconsumedOutput(t *testing.T) {
	s, control := httpInterruptFixture(t, t.Context(),
		`{"type":"response.created","sequence_number":0,"response":{"id":"resp_one","status":"in_progress","output":[]}}`,
		`{"type":"response.output_item.done","sequence_number":5,"output_index":0,"item":{"id":"rs_done","type":"reasoning","encrypted_content":"opaque","summary":[]}}`,
		`{"type":"response.output_item.added","sequence_number":6,"output_index":1,"item":{"id":"fc_partial","type":"function_call","call_id":"call_partial","arguments":""}}`,
		`{"type":"response.completed","response":{"id":"resp_one","status":"completed","output":[{"id":"fc_partial","type":"function_call","arguments":"{\"late\":true}"}]}}`,
	)
	require.False(t, control.InterruptHTTP("foreign"))
	for range 3 {
		require.True(t, s.Next())
		require.NotNil(t, s.Current())
	}
	require.False(t, control.Ready(), "HTTP interrupt support must not advertise response.steer")
	require.True(t, control.InterruptHTTP("resp_one"))
	require.True(t, control.InterruptHTTP("resp_one"), "repeated pending cancellation is idempotent")
	var events [][]byte
	for s.Next() {
		events = append(events, s.Current().Data)
	}
	require.NoError(t, s.Err())
	require.Len(t, events, 3)
	require.Equal(t, "response.interrupt.accepted", gjson.GetBytes(events[0], "type").String())
	require.Equal(t, "response.output_item.interrupted", gjson.GetBytes(events[1], "type").String())
	require.Equal(t, "fc_partial", gjson.GetBytes(events[1], "item_id").String())
	require.Equal(t, "response.incomplete", gjson.GetBytes(events[2], "type").String())
	require.Equal(t, "opaque", gjson.GetBytes(events[2], "response.output.0.encrypted_content").String())
	require.NotContains(t, string(events[2]), "fc_partial")
	require.False(t, gjson.GetBytes(events[2], "response.usage").Exists())
	for i, event := range events {
		require.Equal(t, int64(7+i), gjson.GetBytes(event, "sequence_number").Int())
	}
	require.True(t, control.IsTerminalResponse("resp_one"))
}

func TestHTTPInterruptCompletionAndParentCancellationWin(t *testing.T) {
	t.Run("terminal", func(t *testing.T) {
		s, control := httpInterruptFixture(t, t.Context(),
			`{"type":"response.created","response":{"id":"resp_one","output":[]}}`,
			`{"type":"response.completed","response":{"id":"resp_one","status":"completed","output":[],"usage":{"total_tokens":7}}}`,
		)
		for range 2 {
			require.True(t, s.Next())
			require.NotNil(t, s.Current())
		}
		require.False(t, control.InterruptHTTP("resp_one"))
		require.True(t, control.IsTerminalResponse("resp_one"))
		require.Equal(t, int64(7), gjson.GetBytes(s.Current().Data, "response.usage.total_tokens").Int())
	})
	t.Run("parent", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		s, control := httpInterruptFixture(t, ctx, `{"type":"response.created","response":{"id":"resp_one","output":[]}}`)
		require.True(t, s.Next())
		require.True(t, control.InterruptHTTP("resp_one"))
		cancel()
		require.False(t, s.Next())
		require.ErrorIs(t, s.Err(), context.Canceled)
		require.False(t, control.IsTerminalResponse("resp_one"))
	})
}

func TestHTTPInterruptPreservesFailuresAndRejectsUnsafeSnapshots(t *testing.T) {
	t.Run("source error", func(t *testing.T) {
		s, control := httpInterruptFixture(t, t.Context())
		failure := errors.New("upstream EOF")
		s.source = &httpInterruptErrorStream{err: failure}
		require.False(t, s.Next())
		require.ErrorIs(t, s.Err(), failure)
		require.False(t, control.InterruptHTTP("unknown"))
	})
	for _, body := range []string{
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message"}}`,
		`{"type":"response.output_item.done","output_index":4096,"item":{"type":"message"}}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"message","status":"in_progress"}}`,
		`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","encrypted_content":"` + strings.Repeat("x", 16<<20) + `"}}`,
	} {
		s, control := httpInterruptFixture(t, t.Context(), `{"type":"response.created","response":{"id":"resp_one","output":[]}}`, body)
		for range 2 {
			require.True(t, s.Next())
			require.NotNil(t, s.Current())
		}
		require.False(t, control.InterruptHTTP("resp_one"), "never fabricate an incomplete history after the retention limit")
	}
	t.Run("conflicting completed item", func(t *testing.T) {
		s, control := httpInterruptFixture(t, t.Context(),
			`{"type":"response.created","response":{"id":"resp_one","output":[]}}`,
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_first","encrypted_content":"first"}}`,
			`{"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"rs_second","encrypted_content":"second"}}`,
		)
		for range 3 {
			require.True(t, s.Next())
			require.NotNil(t, s.Current())
		}
		require.False(t, control.InterruptHTTP("resp_one"))
	})
}

type httpInterruptErrorStream struct{ err error }

func (*httpInterruptErrorStream) Next() bool                       { return false }
func (*httpInterruptErrorStream) Current() *httpclient.StreamEvent { return nil }
func (s *httpInterruptErrorStream) Err() error                     { return s.err }
func (*httpInterruptErrorStream) Close() error                     { return nil }
