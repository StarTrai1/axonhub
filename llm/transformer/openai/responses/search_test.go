package responses

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
)

func TestSearchInboundTransformer_TransformRequest(t *testing.T) {
	body := []byte(`{"id":"search-1","model":"gpt-5.6-sol","commands":{"search_query":[{"q":"latest codex"}]},"future_field":{"keep":true}}`)
	inbound := NewSearchInboundTransformer()

	req, err := inbound.TransformRequest(t.Context(), &httpclient.Request{
		Headers: http.Header{"Content-Type": []string{"application/json"}},
		Body:    body,
	})
	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-sol", req.Model)
	require.Equal(t, llm.RequestTypeSearch, req.RequestType)
	require.Equal(t, llm.APIFormatOpenAISearch, req.APIFormat)
	require.Equal(t, body, req.Search.Raw)

	_, err = inbound.TransformRequest(t.Context(), &httpclient.Request{
		Headers: http.Header{"Content-Type": []string{"application/json"}},
		Body:    []byte(`{"commands":{}}`),
	})
	require.ErrorContains(t, err, "model is required")
}

func TestSearchOutboundTransformer_UsesHTTPAndPreservesRequestFields(t *testing.T) {
	outbound, err := NewSearchOutboundTransformer("wss://api.openai.com/v1#", "test-key")
	require.NoError(t, err)

	_, customized := any(outbound).(pipeline.ChannelCustomizedExecutor)
	require.False(t, customized)

	req, err := outbound.TransformRequest(t.Context(), &llm.Request{
		Model:       "mapped-gpt-5.6-sol",
		RequestType: llm.RequestTypeSearch,
		APIFormat:   llm.APIFormatOpenAISearch,
		Search: &llm.SearchRequest{
			Raw: []byte(`{"id":"search-1","model":"gpt-5.6-sol","future_field":{"keep":true}}`),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://api.openai.com/v1/alpha/search", req.URL)
	require.Equal(t, http.MethodPost, req.Method)
	require.Equal(t, "test-key", req.Auth.APIKey)
	require.Equal(t, "mapped-gpt-5.6-sol", gjson.GetBytes(req.Body, "model").String())
	require.True(t, gjson.GetBytes(req.Body, "future_field.keep").Bool())
}

func TestSearchTransformers_RoundTripResponseUnchanged(t *testing.T) {
	body := []byte(`{"encrypted_output":"ciphertext","output":"search result","results":[{"type":"future_result","future_field":{"keep":true}}]}`)
	outbound, err := NewSearchOutboundTransformer("https://api.openai.com/v1", "test-key")
	require.NoError(t, err)

	llmResp, err := outbound.TransformResponse(t.Context(), &httpclient.Response{
		StatusCode: http.StatusOK,
		Body:       body,
		Request: &httpclient.Request{
			Body: []byte(`{"id":"search-1","model":"gpt-5.6-sol"}`),
		},
	})
	require.NoError(t, err)
	require.Equal(t, llm.RequestTypeSearch, llmResp.RequestType)
	require.Equal(t, llm.APIFormatOpenAISearch, llmResp.APIFormat)
	require.Equal(t, body, llmResp.Search.Raw)

	finalResp, err := NewSearchInboundTransformer().TransformResponse(context.Background(), llmResp)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, finalResp.StatusCode)
	require.Equal(t, "application/json", finalResp.Headers.Get("Content-Type"))
	require.Equal(t, body, finalResp.Body)
}
