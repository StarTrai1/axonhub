package gemini_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/gemini"
)

func TestAnthropicToolResultErrorToGemini(t *testing.T) {
	for _, model := range []string{"gemini-2.5-flash", "gemini-3-pro-preview"} {
		for _, tc := range []struct {
			name, text string
			value      any
		}{
			{"text", "permission denied", "permission denied"},
			{"object", `{"code":403,"detail":"denied"}`, map[string]any{"code": float64(403), "detail": "denied"}},
			{"schema reference", `{"schema":{"$ref":"#/definitions/result"}}`, `{"schema":{"$ref":"#/definitions/result"}}`},
		} {
			for _, flag := range []string{"true", "false", "omitted"} {
				t.Run(model+"/"+tc.name+"/"+flag, func(t *testing.T) {
					result := map[string]any{
						"type": "tool_result", "tool_use_id": "call_one",
						"content": []any{
							map[string]any{"type": "text", "text": tc.text},
							map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "aW1hZ2U="}},
						},
					}
					if flag != "omitted" {
						result["is_error"] = flag == "true"
					}
					body, err := json.Marshal(map[string]any{
						"model": "claude-sonnet-5-5", "max_tokens": 100,
						"messages": []any{
							map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": "call_one", "name": "inspect", "input": map[string]any{}}}},
							map[string]any{"role": "user", "content": []any{result}},
						},
					})
					require.NoError(t, err)
					request, err := anthropic.NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{
						Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: body,
					})
					require.NoError(t, err)
					request.Model = model
					adapter, err := gemini.NewOutboundTransformer("", "fixture")
					require.NoError(t, err)
					wire, err := adapter.TransformRequest(t.Context(), request)
					require.NoError(t, err)
					var decoded gemini.GenerateContentRequest
					require.NoError(t, json.Unmarshal(wire.Body, &decoded))
					response := decoded.Contents[1].Parts[0].FunctionResponse
					require.Equal(t, "call_one", response.ID)
					require.Equal(t, "inspect", response.Name)
					if flag == "true" {
						require.Equal(t, map[string]any{"error": tc.value}, response.Response)
					} else if object, ok := tc.value.(map[string]any); ok {
						require.Equal(t, object, response.Response)
					} else {
						require.Equal(t, map[string]any{"result": tc.value}, response.Response)
					}
					if model == "gemini-3-pro-preview" {
						require.Equal(t, "aW1hZ2U=", response.Parts[0].InlineData.Data)
					} else {
						require.Empty(t, response.Parts)
						require.Equal(t, "aW1hZ2U=", decoded.Contents[2].Parts[0].InlineData.Data)
					}
				})
			}
		}
	}
}
