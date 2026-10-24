package gemini

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai"
)

func TestChatImageConfigReachesGeminiAndChat(t *testing.T) {
	for _, config := range []string{`{"aspectRatio":"16:9","imageSize":"2K","futureOption":{"enabled":true}}`, `{}`, `null`} {
		t.Run(config, func(t *testing.T) {
			body := []byte(`{"model":"gemini-image","messages":[{"role":"user","content":"draw"}],"imageConfig":` + config + `}`)
			unified, err := openai.NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: body, Headers: http.Header{"Content-Type": {"application/json"}}})
			require.NoError(t, err)
			// Exercise persisted metadata, not only the in-process RawMessage shape.
			stored, err := json.Marshal(unified.TransformerMetadata)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(stored, &unified.TransformerMetadata))
			out, err := NewOutboundTransformer("https://gemini.example", "fixture")
			require.NoError(t, err)
			req, err := out.TransformRequest(t.Context(), unified)
			require.NoError(t, err)
			chat, err := json.Marshal(openai.RequestFromLLM(t.Context(), unified, openai.ReasoningFieldContent))
			require.NoError(t, err)
			for _, field := range []gjson.Result{gjson.GetBytes(req.Body, "generationConfig.imageConfig"), gjson.GetBytes(chat, "imageConfig")} {
				if config == "null" {
					require.False(t, field.Exists())
				} else {
					require.JSONEq(t, config, field.Raw)
				}
			}
		})
	}
	for _, config := range []string{`[]`, `"invalid"`, `42`} {
		_, err := openai.NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: []byte(`{"model":"test","messages":[{"role":"user","content":"draw"}],"imageConfig":` + config + `}`), Headers: http.Header{"Content-Type": {"application/json"}}})
		require.ErrorContains(t, err, "imageConfig must be an object")
	}
}
