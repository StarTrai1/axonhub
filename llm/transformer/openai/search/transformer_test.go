package search

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/auth"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestInboundTransformRequest(t *testing.T) {
	inbound := NewInboundTransformer()

	req, err := inbound.TransformRequest(context.Background(), &httpclient.Request{
		Headers: http.Header{"Content-Type": []string{"application/json"}},
		Body:    []byte(`{"id":"search-1","model":"gpt-5.6-sol","input":"query"}`),
	})
	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-sol", req.Model)
	require.Equal(t, llm.RequestTypeChat, req.RequestType)
	require.Equal(t, llm.APIFormatOpenAISearch, req.APIFormat)
}

func TestInboundTransformRequestRequiresModel(t *testing.T) {
	inbound := NewInboundTransformer()

	_, err := inbound.TransformRequest(context.Background(), &httpclient.Request{
		Headers: http.Header{"Content-Type": []string{"application/json"}},
		Body:    []byte(`{"id":"search-1","input":"query"}`),
	})
	require.ErrorContains(t, err, "model is required")
}

func TestOutboundPreservesPayloadAndPatchesMappedModel(t *testing.T) {
	outbound, err := NewOutboundTransformerWithConfig(&Config{
		BaseURL:        "https://example.com/v1",
		APIKeyProvider: auth.NewStaticKeyProvider("provider-key"),
	})
	require.NoError(t, err)

	httpReq, err := outbound.TransformRequest(context.Background(), &llm.Request{
		Model:       "mapped-model",
		RequestType: llm.RequestTypeChat,
		APIFormat:   llm.APIFormatOpenAISearch,
		RawRequest: &httpclient.Request{
			Body: []byte(`{"id":"search-1","model":"client-model","input":"query","response_length":"short"}`),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://example.com/v1/alpha/search", httpReq.URL)
	require.Equal(t, "provider-key", httpReq.Auth.APIKey)
	require.JSONEq(t,
		`{"id":"search-1","model":"mapped-model","input":"query","response_length":"short"}`,
		string(httpReq.Body),
	)
}

func TestSearchResponseRoundTrip(t *testing.T) {
	outbound, err := NewOutboundTransformerWithConfig(&Config{
		BaseURL:        "https://example.com/v1",
		APIKeyProvider: auth.NewStaticKeyProvider("provider-key"),
	})
	require.NoError(t, err)

	rawBody := []byte(`{"encrypted_output":"ciphertext","output":"result","results":[]}`)
	llmResp, err := outbound.TransformResponse(context.Background(), &httpclient.Response{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
		Body:       rawBody,
		Request: &httpclient.Request{
			TransformerMetadata: map[string]any{searchModelMetadataKey: "gpt-5.6-sol"},
		},
	})
	require.NoError(t, err)
	require.Equal(t, "gpt-5.6-sol", llmResp.Model)

	httpResp, err := NewInboundTransformer().TransformResponse(context.Background(), llmResp)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, httpResp.StatusCode)
	require.Equal(t, rawBody, httpResp.Body)
	require.Equal(t, "application/json", httpResp.Headers.Get("Content-Type"))
}

func TestOutboundCustomEndpointPath(t *testing.T) {
	outbound, err := NewOutboundTransformerWithConfig(&Config{
		BaseURL:        "https://example.com/proxy",
		EndpointPath:   "/custom/search",
		APIKeyProvider: auth.NewStaticKeyProvider("provider-key"),
	})
	require.NoError(t, err)
	require.Equal(t, "https://example.com/proxy/custom/search", outbound.requestURL())
}

func TestOutboundUsesHTTPForWebSocketResponsesBaseURL(t *testing.T) {
	outbound, err := NewOutboundTransformerWithConfig(&Config{
		BaseURL:        "wss://example.com/v1",
		APIKeyProvider: auth.NewStaticKeyProvider("provider-key"),
	})
	require.NoError(t, err)
	require.Equal(t, "https://example.com/v1/alpha/search", outbound.requestURL())
}
