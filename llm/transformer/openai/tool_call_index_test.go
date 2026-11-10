package openai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestToolCallIndexesKeepReusedUpstreamIndexesSeparate(t *testing.T) {
	for _, indexes := range [][]string{{"0", "0", "1"}, {"4", "8", "9"}, {"null", "null", "null"}} {
		t.Run(indexes[1], func(t *testing.T) {
			var events []*httpclient.StreamEvent
			for i, id := range []string{"call_a", "call_b", "call_c"} {
				for j, args := range []string{`{"q":`, `"` + id + `"}`} {
					call := map[string]any{"index": json.RawMessage(indexes[i]), "function": map[string]any{"arguments": args}}
					if j == 0 {
						call["id"], call["type"] = id, "function"
						call["function"].(map[string]any)["name"] = "search"
					}
					body, err := json.Marshal(map[string]any{"id": "chatcmpl-test", "choices": []any{
						map[string]any{"index": 0, "delta": map[string]any{"tool_calls": []any{call}}},
					}})
					require.NoError(t, err)
					events = append(events, &httpclient.StreamEvent{Data: body})
				}
			}

			body, _, err := AggregateStreamChunks(t.Context(), events, DefaultTransformChunk)
			require.NoError(t, err)
			var response llm.Response
			require.NoError(t, json.Unmarshal(body, &response))
			calls := response.Choices[0].Message.ToolCalls
			require.Len(t, calls, 3)
			for i, id := range []string{"call_a", "call_b", "call_c"} {
				require.Equal(t, id, calls[i].ID)
				require.JSONEq(t, `{"q":"`+id+`"}`, calls[i].Function.Arguments)
			}

			outbound, err := NewOutboundTransformer("https://example.invalid/v1", "test-key")
			require.NoError(t, err)
			stream, err := outbound.TransformStream(t.Context(), nil, streams.SliceStream(events))
			require.NoError(t, err)
			defer stream.Close()
			var emitted []int
			for stream.Next() {
				for _, choice := range stream.Current().Choices {
					for _, call := range choice.Delta.ToolCalls {
						emitted = append(emitted, call.Index)
					}
				}
			}
			require.NoError(t, stream.Err())
			require.Equal(t, []int{calls[0].Index, calls[0].Index, calls[1].Index, calls[1].Index, calls[2].Index, calls[2].Index}, emitted)
		})
	}
}
