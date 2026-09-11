package codex

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

const memoryConsolidationRequestBody = `{
	"model":"gpt-6-astra",
	"stream":true,
	"store":false,
	"reasoning":{"effort":"medium","summary":"auto","context":"all_turns"},
	"client_metadata":{"thread_id":"memory-child","x-openai-subagent":"memory_consolidation"},
	"input":[
		{"type":"additional_tools","role":"developer","tools":[]},
		{"type":"message","role":"developer","content":[{"type":"input_text","text":"Consolidate local memories."}]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"Retain the existing notes."}]}
	]
}`

func TestCodexOutbound_MemgenHeaderIsOfficialOnly(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		baseURL  string
		official bool
	}{
		{name: "official HTTPS", baseURL: "https://chatgpt.com/backend-api/codex#", official: true},
		{name: "official WebSocket URL", baseURL: "wss://chatgpt.com/backend-api/codex#", official: true},
		{name: "official mixed case hostname", baseURL: "https://CHATGPT.COM/backend-api/codex#", official: true},
		{name: "relay HTTPS", baseURL: "https://relay.example/v1"},
		{name: "relay WebSocket URL", baseURL: "wss://relay.example/v1"},
		{name: "relay path contains official hostname", baseURL: "https://relay.example/chatgpt.com/v1"},
		{name: "relay hostname contains official hostname", baseURL: "https://chatgpt.com.relay.example/v1"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			inbound := memoryConsolidationRequest()
			outbound := newTestCodexOutboundForBaseURL(t, testCase.baseURL)
			request, err := responses.NewInboundTransformer().TransformRequest(t.Context(), inbound)
			require.NoError(t, err)
			request.RawRequest = inbound
			providerRequest, err := outbound.TransformRequest(t.Context(), request)
			require.NoError(t, err)

			// Exercise the merge that originally reintroduced this private header.
			providerRequest = httpclient.MergeInboundRequest(providerRequest, inbound)
			providerRequest = outbound.FinalizeTransportRequest(providerRequest)
			if testCase.official {
				require.Equal(t, "true", providerRequest.Headers.Get(MemgenRequestHeader))
			} else {
				require.Empty(t, providerRequest.Headers.Get(MemgenRequestHeader))
			}
			require.Equal(t, "true", inbound.Headers.Get(MemgenRequestHeader))
			require.Equal(t, memoryConsolidationRequestBody, string(inbound.Body))
			require.Equal(t, "memory-session", providerRequest.Headers.Get(SessionHeaderHyphen))
			require.Equal(t, "memory-child", providerRequest.Headers.Get(ThreadIDHeader))
			require.Equal(t, "memory_consolidation", providerRequest.Headers.Get("X-Openai-Subagent"))
			require.Equal(t, "gpt-6-astra", gjson.GetBytes(providerRequest.Body, "model").String())
			require.Equal(t, "medium", gjson.GetBytes(providerRequest.Body, "reasoning.effort").String())
			require.Equal(t, "all_turns", gjson.GetBytes(providerRequest.Body, "reasoning.context").String())
			require.JSONEq(t, gjson.GetBytes(inbound.Body, "client_metadata").Raw, gjson.GetBytes(providerRequest.Body, "client_metadata").Raw)
			require.Equal(t, "additional_tools", gjson.GetBytes(providerRequest.Body, "input.0.type").String())
		})
	}
}

func TestCodexOutbound_RelayMemoryRequestOnWire(t *testing.T) {
	t.Parallel()

	type capturedRequest struct {
		headers http.Header
		body    []byte
		err     error
	}
	captured := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		captured <- capturedRequest{headers: request.Header.Clone(), body: body, err: err}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(writer, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_memory\",\"status\":\"completed\",\"model\":\"gpt-6-astra\",\"output\":[]}}\n\n")
	}))
	defer server.Close()

	inbound := memoryConsolidationRequest()
	// Header merging must also reject noncanonical spellings from adapters.
	inbound.Headers.Del(MemgenRequestHeader)
	inbound.Headers["x-openai-memgen-request"] = []string{"true"}
	outbound := newTestCodexOutboundForBaseURL(t, server.URL)
	request, err := responses.NewInboundTransformer().TransformRequest(t.Context(), inbound)
	require.NoError(t, err)
	request.RawRequest = inbound
	providerRequest, err := outbound.TransformRequest(t.Context(), request)
	require.NoError(t, err)
	providerRequest = httpclient.MergeInboundRequest(providerRequest, inbound)
	providerRequest = outbound.FinalizeTransportRequest(providerRequest)

	client := httpclient.NewHttpClientWithClient(server.Client())
	stream, err := client.DoStream(t.Context(), providerRequest)
	require.NoError(t, err)
	defer stream.Close()
	require.True(t, stream.Next())
	require.Equal(t, "response.completed", stream.Current().Type)

	actual := <-captured
	require.NoError(t, actual.err)
	for name := range actual.headers {
		require.False(t, strings.EqualFold(name, MemgenRequestHeader))
	}
	require.Equal(t, []string{"true"}, inbound.Headers["x-openai-memgen-request"])
	require.Equal(t, "memory_consolidation", actual.headers.Get("X-Openai-Subagent"))
	require.Equal(t, "gpt-6-astra", gjson.GetBytes(actual.body, "model").String())
	require.Equal(t, "medium", gjson.GetBytes(actual.body, "reasoning.effort").String())
	require.JSONEq(t, gjson.GetBytes(inbound.Body, "client_metadata").Raw, gjson.GetBytes(actual.body, "client_metadata").Raw)
}

func memoryConsolidationRequest() *httpclient.Request {
	headers := make(http.Header)
	headers.Set("Content-Type", "application/json")
	headers.Set(MemgenRequestHeader, "true")
	headers.Set("X-Openai-Subagent", "memory_consolidation")
	headers.Set(SessionHeaderHyphen, "memory-session")
	headers.Set(ThreadIDHeader, "memory-child")
	headers.Set("Version", "0.154.0")
	return &httpclient.Request{Method: http.MethodPost, Headers: headers, Body: []byte(memoryConsolidationRequestBody)}
}
