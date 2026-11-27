package responses

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestAggregatedOutputTextAlwaysCarriesAnnotations(t *testing.T) {
	for _, annotations := range [][]Annotation{nil, {}, {{Type: "url_citation", URLCitation: &URLCitation{URL: "https://example.com", Title: "source"}}}} {
		terminal := StreamEvent{Type: StreamEventTypeResponseCompleted, Response: &Response{
			ID: "resp_shape", Model: "test", Status: lo.ToPtr("completed"),
			Output: []Item{{ID: "msg_shape", Type: "message", Role: "assistant", Content: &Input{Items: []Item{{Type: "output_text", Text: lo.ToPtr("answer"), Annotations: annotations}}}}},
		}}
		data, err := json.Marshal(terminal)
		require.NoError(t, err)
		body, _, err := AggregateStreamChunks(t.Context(), []*httpclient.StreamEvent{{Data: data}})
		require.NoError(t, err)
		content := gjson.GetBytes(body, "output.0.content.0")
		require.Equal(t, "answer", content.Get("text").String())
		require.True(t, content.Get("annotations").IsArray())
		require.Len(t, content.Get("annotations").Array(), len(annotations))
		var output Item
		require.NoError(t, json.Unmarshal([]byte(content.Raw), &output))
		if len(annotations) > 0 {
			require.Equal(t, annotations, output.Annotations)
		}
	}
}

func TestOutputTextHistoryDoesNotGainAnnotations(t *testing.T) {
	for _, kind := range []string{"input_text", "output_text"} {
		input, err := json.Marshal(Item{Type: kind, Text: lo.ToPtr("original history")})
		require.NoError(t, err)
		require.False(t, gjson.GetBytes(input, "annotations").Exists())
	}
}
