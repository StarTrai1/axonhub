package responses

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestResponsesCustomToolDoneRecoversMissingInput(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		delta   string
		final   string
		callID  bool
		want    string
		wantErr bool
	}{
		{name: "done only", final: "print('ok')", want: "print('ok')"},
		{name: "partial delta", delta: "print(", final: "print('ok')", want: "print('ok')"},
		{name: "complete delta", delta: "print('ok')", final: "print('ok')", want: "print('ok')"},
		{name: "empty done", delta: "print('ok')", want: "print('ok')"},
		{name: "call id only", final: "print('ok')", callID: true, want: "print('ok')"},
		{name: "conflicting done", delta: "first", final: "second", wantErr: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			events := []*httpclient.StreamEvent{
				{Data: []byte(`{"type":"response.created","response":{"id":"resp_custom","model":"gpt-6-astra","status":"in_progress","output":[]}}`)},
				{Data: []byte(`{"type":"response.output_item.added","output_index":0,"item":{"id":"ctc_native","type":"custom_tool_call","call_id":"call_custom","name":"exec","input":"","async":true}}`)},
			}
			if scenario.delta != "" {
				data, err := json.Marshal(map[string]any{"type": "response.custom_tool_call_input.delta", "item_id": "ctc_native", "output_index": 0, "delta": scenario.delta})
				require.NoError(t, err)
				events = append(events, &httpclient.StreamEvent{Data: data})
			}
			done := map[string]any{"type": "response.custom_tool_call_input.done", "item_id": "ctc_native", "output_index": 0, "input": scenario.final}
			if scenario.callID {
				delete(done, "item_id")
				done["call_id"] = "call_custom"
			}
			data, err := json.Marshal(done)
			require.NoError(t, err)
			events = append(events, &httpclient.StreamEvent{Data: data}, &httpclient.StreamEvent{Data: data}, &httpclient.StreamEvent{Data: []byte(`{"type":"response.completed","response":{"id":"resp_custom","status":"completed","output":[]}}`)})
			stream := newResponsesOutboundStream(streams.SliceStream(events))
			converted, err := streams.All(stream)
			if scenario.wantErr {
				require.ErrorContains(t, err, "input changed after streaming began")
				return
			}
			require.NoError(t, err)
			input := ""
			for _, response := range converted {
				if response == llm.DoneResponse {
					continue
				}
				for _, choice := range response.Choices {
					if choice.Delta == nil {
						continue
					}
					for _, toolCall := range choice.Delta.ToolCalls {
						if toolCall.ResponseCustomToolCall != nil {
							input += toolCall.ResponseCustomToolCall.Input
						}
					}
				}
			}
			require.Equal(t, scenario.want, input)
			require.Equal(t, scenario.want, stream.state.toolCalls["call_custom"].ResponseCustomToolCall.Input)
			require.NotNil(t, stream.state.toolCalls["call_custom"].Async)
			require.True(t, *stream.state.toolCalls["call_custom"].Async)
		})
	}
}
