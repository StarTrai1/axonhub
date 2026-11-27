package responses

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestOutputTextAlwaysCarriesAnnotations(t *testing.T) {
	for _, annotations := range [][]Annotation{nil, {}, {{Type: "url_citation", URLCitation: &URLCitation{URL: "https://example.com", Title: "source"}}}} {
		item := Item{Type: "output_text", Text: lo.ToPtr("answer"), Annotations: annotations}
		body, err := json.Marshal(item)
		require.NoError(t, err)
		require.True(t, gjson.GetBytes(body, "annotations").IsArray())
		require.Len(t, gjson.GetBytes(body, "annotations").Array(), len(annotations))
		require.False(t, gjson.GetBytes(body, "summary").Exists())
	}
	input, err := json.Marshal(Item{Type: "input_text", Text: lo.ToPtr("original prompt")})
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(input, "annotations").Exists())
}

func TestAggregatedOutputTextHasAnnotationsWhenUpstreamOmitsThem(t *testing.T) {
	events := []*httpclient.StreamEvent{
		{Data: []byte(`{"type":"response.created","response":{"id":"resp_shape","model":"test","status":"in_progress"}}`)},
		{Data: []byte(`{"type":"response.completed","response":{"id":"resp_shape","model":"test","status":"completed","output":[{"id":"msg_shape","type":"message","role":"assistant","content":[{"type":"output_text","text":"answer"}]}]}}`)},
	}
	response, _, err := (&InboundTransformer{}).AggregateStreamChunks(t.Context(), events)
	require.NoError(t, err)
	require.Equal(t, "answer", gjson.GetBytes(response, "output.0.content.0.text").String())
	require.Equal(t, "[]", gjson.GetBytes(response, "output.0.content.0.annotations").Raw)
}
