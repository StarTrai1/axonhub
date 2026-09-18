package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
)

func TestXAIHostedToolChoiceKeepsSelection(t *testing.T) {
	for _, test := range []struct {
		name     string
		choice   string
		toolType string
		mode     string
	}{
		{"forced web", `{"type":"web_search"}`, "web_search", "required"},
		{"forced image", `{"type":"image_generation"}`, "image_generation", "required"},
		{"allowed required", `{"type":"allowed_tools","mode":"required","tools":[{"type":"web_search"}]}`, "web_search", "required"},
		{"allowed auto", `{"type":"allowed_tools","mode":"auto","tools":[{"type":"image_generation"}]}`, "image_generation", "auto"},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(`{"model":"grok-4.6","input":"hello","precision":9007199254740993,"tool_choice":` + test.choice + `,"tools":[{"type":"web_search","filters":{"allowed_domains":["example.com"]}},{"type":"image_generation","quality":"high"},{"type":"x_search"},{"type":"function","name":"run"}]}`)
			original := append([]byte(nil), body...)
			outbound := &PersistentOutboundTransformer{state: &PersistenceState{
				CurrentCandidate: &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{Type: channel.TypeXaiSubscription}}},
			}}
			request := &httpclient.Request{APIFormat: string(llm.APIFormatOpenAIResponse), Body: body}
			result, err := applyXAIHostedToolChoice(outbound).OnOutboundRawRequest(t.Context(), request)
			require.NoError(t, err)
			require.Equal(t, test.mode, gjson.GetBytes(result.Body, "tool_choice").String())
			tools := gjson.GetBytes(result.Body, "tools").Array()
			require.Len(t, tools, 1)
			require.Equal(t, test.toolType, tools[0].Get("type").String())
			require.Equal(t, "9007199254740993", gjson.GetBytes(result.Body, "precision").Raw)
			if test.toolType == "web_search" {
				require.Equal(t, "example.com", tools[0].Get("filters.allowed_domains.0").String())
			} else {
				require.Equal(t, "high", tools[0].Get("quality").String())
			}
			require.Equal(t, original, body)
		})
	}
}

func TestXAIHostedToolChoicePreservesOtherSemantics(t *testing.T) {
	outbound := &PersistentOutboundTransformer{state: &PersistenceState{
		CurrentCandidate: &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{Type: channel.TypeXaiSubscription}}},
	}}
	for _, body := range []string{
		`{"tool_choice":"none","tools":[{"type":"web_search"}]}`,
		`{"tool_choice":{"type":"function","name":"run"},"tools":[{"type":"web_search"}]}`,
		`{"tool_choice":{"type":"allowed_tools","mode":"required","tools":[{"type":"web_search"},{"type":"function","name":"run"}]},"tools":[{"type":"web_search"},{"type":"function","name":"run"}]}`,
	} {
		request := &httpclient.Request{APIFormat: string(llm.APIFormatOpenAIResponse), Body: []byte(body)}
		result, err := applyXAIHostedToolChoice(outbound).OnOutboundRawRequest(t.Context(), request)
		require.NoError(t, err)
		require.Equal(t, body, string(result.Body))
	}
	request := &httpclient.Request{APIFormat: string(llm.APIFormatOpenAIResponse), Body: []byte(`{"tool_choice":{"type":"web_search"},"tools":[]}`)}
	_, err := applyXAIHostedToolChoice(outbound).OnOutboundRawRequest(t.Context(), request)
	require.ErrorIs(t, err, transformer.ErrInvalidRequest)

	// Other endpoints may support native hosted choices and must not be narrowed.
	outbound.state.CurrentCandidate.Channel.Type = channel.TypeXaiResponses
	result, err := applyXAIHostedToolChoice(outbound).OnOutboundRawRequest(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, request.Body, result.Body)
}
