package antigravity

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/gemini"
)

func TestAntigravityPatchingPreservesToolResultError(t *testing.T) {
	body := []byte(`{"model":"claude-sonnet-5-5","max_tokens":100,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_one","name":"inspect","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_one","is_error":true,"content":"permission denied"}]}]}`)
	request, err := anthropic.NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{
		Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: body,
	})
	require.NoError(t, err)
	request.Model = "gemini-3-pro-preview"
	adapter, err := NewTransformer(Config{})
	require.NoError(t, err)
	wire, err := adapter.geminiTransformer.TransformRequest(t.Context(), request)
	require.NoError(t, err)
	var decoded gemini.GenerateContentRequest
	require.NoError(t, json.Unmarshal(wire.Body, &decoded))
	require.NoError(t, adapter.patchGeminiRequest(t.Context(), &decoded, request))
	response := decoded.Contents[1].Parts[0].FunctionResponse
	require.Equal(t, "call_one", response.ID)
	require.Equal(t, "inspect", response.Name)
	require.Equal(t, map[string]any{"error": "permission denied"}, response.Response)
}
