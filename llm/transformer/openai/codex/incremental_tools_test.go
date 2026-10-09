package codex

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestCodex162LiteContinuationDoesNotInjectCatalog(t *testing.T) {
	request := &httpclient.Request{
		Headers: http.Header{},
		Body: []byte(`{"model":"gpt-6.1-sol","stream":true,"previous_response_id":"resp_catalog","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Continue."}]}]}`),
	}
	request.Headers.Set(responses.ResponsesLiteHeader, "true")
	unified, err := responses.NewInboundTransformer().TransformRequest(t.Context(), request)
	require.NoError(t, err)
	unified.RawRequest = request
	result, err := newTestCodexOutbound(t).TransformRequest(t.Context(), unified)
	require.NoError(t, err)
	require.Equal(t, "resp_catalog", gjson.GetBytes(result.Body, "previous_response_id").String())
	require.Len(t, gjson.GetBytes(result.Body, "input").Array(), 1)
	require.Equal(t, "message", gjson.GetBytes(result.Body, "input.0.type").String())
}
