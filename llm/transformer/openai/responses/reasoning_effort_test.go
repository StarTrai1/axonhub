package responses

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestNumericReasoningEffortRoundTrip(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://example.invalid/v1", "synthetic")
	require.NoError(t, err)
	for _, effort := range []string{`0`, `32768`, `18446744073709551615`, `"32768"`, `"high"`} {
		t.Run(effort, func(t *testing.T) {
			raw := []byte(`{"model":"gpt-6-sol","input":"hello","reasoning":{"effort":` + effort + `,"summary":"auto","context":"all_turns"}}`)
			request, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: raw})
			require.NoError(t, err)
			request.ProviderExtensions = llm.CloneProviderExtensions(request.ProviderExtensions)
			wire, err := outbound.TransformRequest(t.Context(), request)
			require.NoError(t, err)
			require.Equal(t, effort, gjson.GetBytes(wire.Body, "reasoning.effort").Raw)
			require.Equal(t, "all_turns", gjson.GetBytes(wire.Body, "reasoning.context").String())
			request.ReasoningEffort = "low"
			mapped, err := outbound.TransformRequest(t.Context(), request)
			require.NoError(t, err)
			require.Equal(t, `"low"`, gjson.GetBytes(mapped.Body, "reasoning.effort").Raw)

			var reasoning ResponseReasoning
			require.NoError(t, json.Unmarshal([]byte(`{"effort":`+effort+`}`), &reasoning))
			encoded, err := json.Marshal(reasoning)
			require.NoError(t, err)
			require.Equal(t, effort, gjson.GetBytes(encoded, "effort").Raw)
		})
	}
}

func TestNumericReasoningEffortRejectsInvalidTypes(t *testing.T) {
	for _, effort := range []string{`-1`, `1.5`, `true`, `{}`, `[]`, `18446744073709551616`} {
		t.Run(effort, func(t *testing.T) {
			var reasoning Reasoning
			require.Error(t, json.Unmarshal([]byte(`{"effort":`+effort+`}`), &reasoning))
		})
	}
}

func TestNumericReasoningEffortInResponseDoesNotBreakCompletion(t *testing.T) {
	outbound, err := NewOutboundTransformer("https://example.invalid/v1", "synthetic")
	require.NoError(t, err)
	body := `{"id":"resp_numeric","model":"gpt-6-sol","status":"completed","reasoning":{"effort":32768},"output":[{"id":"msg_numeric","type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}]}`
	response, err := outbound.TransformResponse(t.Context(), &httpclient.Response{StatusCode: http.StatusOK, Body: []byte(body)})
	require.NoError(t, err)
	require.NotNil(t, response)
	stream, err := outbound.TransformStream(t.Context(), nil, streams.SliceStream([]*httpclient.StreamEvent{{
		Type: "response.completed", Data: []byte(`{"type":"response.completed","response":` + body + `}`),
	}}))
	require.NoError(t, err)
	chunks, err := streams.All(stream)
	require.NoError(t, err)
	require.Contains(t, chunks, llm.DoneResponse)
}

func TestFlexUnavailableStreamPreservesCodeAndStatus(t *testing.T) {
	for _, raw := range []string{
		`{"type":"error","error":{"code":"flex_unavailable","message":"Flex capacity unavailable."}}`,
		`{"type":"response.failed","response":{"id":"resp_flex","status":"failed","error":{"code":"flex_unavailable","message":"Flex capacity unavailable."}}}`,
	} {
		stream := newResponsesOutboundStream(streams.SliceStream([]*httpclient.StreamEvent{{Data: []byte(raw)}}))
		chunks, err := streams.All(stream)
		var responseErr *llm.ResponseError
		require.ErrorAs(t, err, &responseErr)
		require.Equal(t, http.StatusTooManyRequests, responseErr.StatusCode)
		require.Equal(t, "flex_unavailable", responseErr.Detail.Code)
		require.NotContains(t, chunks, llm.DoneResponse)
	}
}
