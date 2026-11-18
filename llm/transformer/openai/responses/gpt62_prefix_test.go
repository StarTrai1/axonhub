package responses

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestGPT62TranslatedSystemPrefixRemainsCacheable(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://example.invalid/v1", "synthetic")
	require.NoError(t, err)
	for _, model := range []string{"gpt-6.2-sol", "gpt-6.2-astra", "gpt-6-astra"} {
		t.Run(model, func(t *testing.T) {
			request := &llm.Request{
				Model: model, APIFormat: llm.APIFormatOpenAIChatCompletion,
				Messages: []llm.Message{
					{Role: "system", Content: llm.MessageContent{Content: lo.ToPtr("stable instructions")}},
					{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("first turn")}},
					{Role: "system", Content: llm.MessageContent{Content: lo.ToPtr("later guidance")}},
					{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("next turn")}},
				},
			}
			wire, err := outbound.TransformRequest(t.Context(), request)
			require.NoError(t, err)
			if model == "gpt-6-astra" {
				require.Equal(t, "stable instructions", gjson.GetBytes(wire.Body, "instructions").String())
				require.Equal(t, "user", gjson.GetBytes(wire.Body, "input.0.role").String())
			} else {
				require.Empty(t, gjson.GetBytes(wire.Body, "instructions").String())
				require.Equal(t, "developer", gjson.GetBytes(wire.Body, "input.0.role").String())
				require.Equal(t, "stable instructions", gjson.GetBytes(wire.Body, "input.0.content.0.text").String())
				require.Equal(t, "later guidance", gjson.GetBytes(wire.Body, "input.2.content.0.text").String())
			}
			require.Equal(t, "system", request.Messages[0].Role, "conversion must not mutate caller history")
		})
	}
}

func TestGPT62NativeInstructionsStayPerRequest(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://example.invalid/v1", "synthetic")
	require.NoError(t, err)
	body := []byte(`{"model":"gpt-6.2-sol","instructions":"only this turn","input":[{"role":"developer","content":"stable prefix"},{"role":"user","content":"continue"}]}`)
	request, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: body})
	require.NoError(t, err)
	wire, err := outbound.TransformRequest(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, "only this turn", gjson.GetBytes(wire.Body, "instructions").String())
	require.JSONEq(t, gjson.GetBytes(body, "input").Raw, gjson.GetBytes(wire.Body, "input").Raw)
}
