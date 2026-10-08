package responses

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestAccessProgramsSurviveResponseConversion(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://api.example/v1", "synthetic-key")
	require.NoError(t, err)
	inbound := NewInboundTransformer()
	for _, selection := range []string{"", `null`, `{"cyber":"standard"}`, `{"cyber":"daybreak_blue"}`} {
		t.Run(selection, func(t *testing.T) {
			response := Response{ID: "resp_program", Model: "gpt-6.1-sol", Object: "response", AccessPrograms: json.RawMessage(selection),
				Output: []Item{{Type: "message", Role: "assistant", Content: &Input{Items: []Item{{Type: "output_text", Text: lo.ToPtr("OK")}}}}}}
			body, err := json.Marshal(response)
			require.NoError(t, err)
			unified, err := outbound.TransformResponse(t.Context(), &httpclient.Response{StatusCode: 200, Body: body})
			require.NoError(t, err)
			wire, err := inbound.TransformResponse(t.Context(), unified)
			require.NoError(t, err)
			require.Equal(t, selection, gjson.GetBytes(wire.Body, "access_programs").Raw)
			// Responses-only metadata must not leak into Chat Completions serialization.
			serialized, err := json.Marshal(unified)
			require.NoError(t, err)
			require.False(t, gjson.GetBytes(serialized, "access_programs").Exists())
		})
	}
}

func TestAccessProgramsSurviveStreamingAndAggregation(t *testing.T) {
	for _, selected := range []string{`null`, `{"cyber":"daybreak_blue"}`} {
		t.Run(selected, func(t *testing.T) {
			// The terminal selection is authoritative even when created used another
			// value, and an explicit null must replace the previous non-null value.
			events := []*httpclient.StreamEvent{
				{Type: "response.created", Data: []byte(`{"type":"response.created","response":{"id":"resp_program","model":"gpt-6.1-sol","status":"in_progress","output":[],"access_programs":{"cyber":"standard"}}}`)},
				{Type: "response.output_text.delta", Data: []byte(`{"type":"response.output_text.delta","output_index":0,"content_index":0,"item_id":"msg_program","delta":"OK"}`)},
				{Type: "response.completed", Data: []byte(`{"type":"response.completed","response":{"id":"resp_program","model":"gpt-6.1-sol","status":"completed","output":[{"type":"message","id":"msg_program","role":"assistant","content":[{"type":"output_text","text":"OK"}]}],"access_programs":` + selected + `}}`)},
			}
			outbound, err := NewOutboundTransformer("https://api.example/v1", "synthetic-key")
			require.NoError(t, err)
			unified, err := outbound.TransformStream(t.Context(), &httpclient.Request{}, streams.SliceStream(events))
			require.NoError(t, err)
			converted, err := NewInboundTransformer().TransformStream(t.Context(), unified)
			require.NoError(t, err)
			wire, err := streams.All(converted)
			require.NoError(t, err)
			require.NoError(t, converted.Close())
			completed := false
			for _, event := range wire {
				if event.Type == "response.completed" {
					completed = true
					require.JSONEq(t, selected, gjson.GetBytes(event.Data, "response.access_programs").Raw)
				}
			}
			require.True(t, completed)
			for _, source := range [][]*httpclient.StreamEvent{events, wire} {
				body, _, err := AggregateStreamChunks(t.Context(), source)
				require.NoError(t, err)
				require.JSONEq(t, selected, gjson.GetBytes(body, "access_programs").Raw)
			}
		})
	}
}
