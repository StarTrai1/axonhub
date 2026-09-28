package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestToolInputOnBlockStartSurvivesStreamAndAggregation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		seed   string
		deltas []string
		want   string
	}{
		{"inline only", `{"query":"seed"}`, nil, `{"query":"seed"}`},
		{"empty delta keeps seed", `{"query":"seed"}`, []string{""}, `{"query":"seed"}`},
		{"deltas replace seed", `{"query":"seed"}`, []string{`{"query":`, `"delta"}`}, `{"query":"delta"}`},
		{"canonical deltas", `{}`, []string{`{"query":"delta"}`}, `{"query":"delta"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := []*httpclient.StreamEvent{
				{Type: "message_start", Data: []byte(`{"type":"message_start","message":{"id":"msg_seed","role":"assistant","model":"claude-test","content":[]}}`)},
				{Type: "content_block_start", Data: []byte(fmt.Sprintf(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_seed","name":"search","input":%s}}`, tc.seed))},
			}
			for _, delta := range tc.deltas {
				encoded, err := json.Marshal(delta)
				require.NoError(t, err)
				events = append(events, &httpclient.StreamEvent{Type: "content_block_delta", Data: []byte(fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":%s}}`, encoded))})
			}
			events = append(events,
				&httpclient.StreamEvent{Type: "content_block_stop", Data: []byte(`{"type":"content_block_stop","index":0}`)},
				&httpclient.StreamEvent{Type: "content_block_start", Data: []byte(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"tool_next","name":"next","input":{"next":true}}}`)},
				&httpclient.StreamEvent{Type: "content_block_stop", Data: []byte(`{"type":"content_block_stop","index":1}`)},
				&httpclient.StreamEvent{Type: "message_delta", Data: []byte(`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":10}}`)},
				&httpclient.StreamEvent{Type: "message_stop", Data: []byte(`{"type":"message_stop"}`)},
			)
			outbound := &OutboundTransformer{config: &Config{Type: PlatformDirect}}
			stream, err := outbound.TransformStream(t.Context(), nil, streams.SliceStream(events))
			require.NoError(t, err)
			defer stream.Close()
			arguments := make(map[string]*strings.Builder)
			for stream.Next() {
				for _, choice := range stream.Current().Choices {
					if choice.Delta == nil {
						continue
					}
					for _, call := range choice.Delta.ToolCalls {
						if arguments[call.ID] == nil {
							arguments[call.ID] = &strings.Builder{}
						}
						arguments[call.ID].WriteString(call.Function.Arguments)
					}
				}
			}
			require.NoError(t, stream.Err())
			require.JSONEq(t, tc.want, arguments["tool_seed"].String())
			require.JSONEq(t, `{"next":true}`, arguments["tool_next"].String())
			body, meta, err := AggregateStreamChunks(t.Context(), events, PlatformDirect)
			require.NoError(t, err)
			require.True(t, meta.Completed)
			var response Message
			require.NoError(t, json.Unmarshal(body, &response))
			require.JSONEq(t, tc.want, string(response.Content[0].Input))
			require.JSONEq(t, `{"next":true}`, string(response.Content[1].Input))
		})
	}
}
