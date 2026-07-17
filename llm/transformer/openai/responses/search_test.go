package responses

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/auth"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
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
	require.True(t, customized)

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

func TestSearchOutboundTransformer_FallsBackToNonStreamingResponsesAndCachesCapability(t *testing.T) {
	outbound, err := NewSearchOutboundTransformerWithConfig(&SearchConfig{
		BaseURL:                  "https://search.example.test/v1",
		APIKeyProvider:           auth.NewStaticKeyProvider("test-key"),
		ResponsesBaseURL:         "https://responses.example.test/v1",
		ResponsesEndpointPath:    "/custom/responses",
		CapabilityNegativeTTL:    time.Hour,
	})
	require.NoError(t, err)

	searchRequest, err := outbound.TransformRequest(t.Context(), &llm.Request{
		Model:       "gpt-5.6-sol",
		RequestType: llm.RequestTypeSearch,
		APIFormat:   llm.APIFormatOpenAISearch,
		Search: &llm.SearchRequest{Raw: []byte(`{
			"id":"search-1",
			"model":"gpt-5.6-sol",
			"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"latest codex"}]}],
			"commands":{"search_query":[{"q":"latest codex"}]},
			"settings":{"search_context_size":"medium","external_web_access":true,"filters":{"allowed_domains":["github.com"]}},
			"max_output_tokens":1200
		}`)},
	})
	require.NoError(t, err)

	responsesBody := []byte(`{
		"id":"resp-1",
		"model":"gpt-5.5",
		"status":"completed",
		"output":[
			{"type":"web_search_call","action":{"type":"search","sources":[{"type":"url","title":"Codex releases","url":"https://github.com/openai/codex/releases"}]}},
			{"type":"message","content":[{"type":"output_text","text":"Latest stable Codex release is 0.144.5.","annotations":[{"type":"url_citation","title":"Codex releases","url":"https://github.com/openai/codex/releases"}]}]}
		]
	}`)
	executor := &queuedSearchExecutor{results: []queuedSearchResult{
		{err: &httpclient.Error{
			Method:     http.MethodPost,
			URL:        "https://search.example.test/v1/alpha/search",
			StatusCode: http.StatusNotFound,
			Body:       []byte(`{"error":{"message":"Invalid URL (POST /v1/alpha/search)"}}`),
		}},
		{response: &httpclient.Response{StatusCode: http.StatusOK, Body: responsesBody}},
		{response: &httpclient.Response{StatusCode: http.StatusOK, Body: responsesBody}},
	}}

	custom := outbound.CustomizeExecutor(executor)
	first, err := custom.Do(t.Context(), searchRequest)
	require.NoError(t, err)
	second, err := custom.Do(t.Context(), searchRequest)
	require.NoError(t, err)

	require.Len(t, executor.requests, 3)
	require.Equal(t, "https://search.example.test/v1/alpha/search", executor.requests[0].URL)
	require.Equal(t, "https://responses.example.test/v1/custom/responses", executor.requests[1].URL)
	require.Equal(t, "https://responses.example.test/v1/custom/responses", executor.requests[2].URL)
	require.Equal(t, "gpt-5.5", gjson.GetBytes(executor.requests[1].Body, "model").String())
	require.False(t, gjson.GetBytes(executor.requests[1].Body, "stream").Bool())
	require.Equal(t, "web_search", gjson.GetBytes(executor.requests[1].Body, "tools.0.type").String())
	require.Equal(t, "medium", gjson.GetBytes(executor.requests[1].Body, "tools.0.search_context_size").String())
	require.Equal(t, "github.com", gjson.GetBytes(executor.requests[1].Body, "tools.0.filters.allowed_domains.0").String())
	require.Contains(t, gjson.GetBytes(executor.requests[1].Body, "input").String(), "latest codex")
	require.Equal(t, "test-key", executor.requests[1].Auth.APIKey)

	for _, response := range []*httpclient.Response{first, second} {
		require.Equal(t, "Latest stable Codex release is 0.144.5.", gjson.GetBytes(response.Body, "output").String())
		require.Equal(t, "text_result", gjson.GetBytes(response.Body, "results.0.type").String())
		require.Equal(t, "turn0search0", gjson.GetBytes(response.Body, "results.0.ref_id").String())
		require.Equal(t, "https://github.com/openai/codex/releases", gjson.GetBytes(response.Body, "results.0.url").String())
	}
}

func TestSearchFallbackDoesNotMaskUnrelatedErrors(t *testing.T) {
	outbound, err := NewSearchOutboundTransformer("https://api.example.test/v1", "test-key")
	require.NoError(t, err)

	executor := &queuedSearchExecutor{results: []queuedSearchResult{{err: &httpclient.Error{
		Method:     http.MethodPost,
		URL:        "https://api.example.test/v1/alpha/search",
		StatusCode: http.StatusNotFound,
		Body:       []byte(`{"error":{"message":"model not found"}}`),
	}}}}
	request := &httpclient.Request{
		Method:  http.MethodPost,
		URL:     "https://api.example.test/v1/alpha/search",
		Headers: make(http.Header),
		Body:    []byte(`{"id":"search-1","model":"gpt-5.6-sol"}`),
	}

	response, err := outbound.CustomizeExecutor(executor).Do(t.Context(), request)
	require.Nil(t, response)
	require.Error(t, err)
	require.Len(t, executor.requests, 1)
}

func TestNormalizeResponsesSearchFallbackUsesLastAssistantMessage(t *testing.T) {
	body, err := normalizeResponsesSearchFallback([]byte(`{
		"status":"completed",
		"output":[
			{"type":"message","content":[{"type":"output_text","text":"I will search now."}]},
			{"type":"message","content":[{"type":"output_text","text":"Final result.","annotations":[{"type":"url_citation","url":"https://example.com","title":"Example"}]}]}
		]
	}`))
	require.NoError(t, err)
	require.Equal(t, "Final result.", gjson.GetBytes(body, "output").String())
	require.Equal(t, "Example", gjson.GetBytes(body, "results.0.title").String())
}

func TestNormalizeSearchFallbackExternalWebAccess(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "live mode", input: `"live"`, expected: "true"},
		{name: "indexed mode", input: `"indexed"`, expected: "false"},
		{name: "cached mode", input: `"cached"`, expected: "false"},
		{name: "boolean", input: "true", expected: "true"},
		{name: "unknown", input: `"unknown"`, expected: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actual := normalizeSearchFallbackExternalWebAccess(json.RawMessage(test.input))
			require.Equal(t, test.expected, string(actual))
		})
	}
}

type queuedSearchResult struct {
	response *httpclient.Response
	err      error
}

type queuedSearchExecutor struct {
	requests []*httpclient.Request
	results  []queuedSearchResult
}

func (e *queuedSearchExecutor) Do(
	_ context.Context,
	request *httpclient.Request,
) (*httpclient.Response, error) {
	e.requests = append(e.requests, request)
	if len(e.results) == 0 {
		return nil, errors.New("no queued search result")
	}

	result := e.results[0]
	e.results = e.results[1:]
	if result.response != nil {
		result.response.Request = request
	}

	return result.response, result.err
}

func (e *queuedSearchExecutor) DoStream(
	context.Context,
	*httpclient.Request,
) (streams.Stream[*httpclient.StreamEvent], error) {
	return nil, errors.New("streaming should not be used")
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
