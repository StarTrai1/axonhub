package openai

import (
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestInboundLegacyFunctionHistory(t *testing.T) {
	body := []byte(`{"model":"test","messages":[
	{"role":"assistant","tool_calls":[{"id":"call_legacy_0","type":"function","function":{"name":"modern","arguments":"{}"}}]},
	{"role":"tool","tool_call_id":"call_legacy_0","content":"modern result"},
	{"role":"assistant","content":"first","function_call":{"name":"lookup","arguments":"{\"i\":1}"}},
	{"role":"assistant","function_call":{"name":"lookup","arguments":"{\"i\":2}"}},
	{"role":"function","name":"lookup","content":"one"},
	{"role":"function","name":"lookup","content":"two"},
	{"role":"user","content":"continue"}]}`)
	req, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{
		Headers: http.Header{"Content-Type": {"application/json"}}, Body: body,
	})
	require.NoError(t, err)
	require.Equal(t, "call_legacy_0", req.Messages[0].ToolCalls[0].ID)
	for i := range 2 {
		call := req.Messages[2+i].ToolCalls[0]
		result := req.Messages[4+i]
		require.Equal(t, "tool", result.Role)
		require.Equal(t, call.ID, lo.FromPtr(result.ToolCallID))
		require.NotEqual(t, "call_legacy_0", call.ID)
	}
	require.NotEqual(t, req.Messages[2].ToolCalls[0].ID, req.Messages[3].ToolCalls[0].ID)
	require.Equal(t, `{"i":1}`, req.Messages[2].ToolCalls[0].Function.Arguments)
	require.Equal(t, "first", lo.FromPtr(req.Messages[2].Content.Content))
	require.Equal(t, body, req.RawRequest.Body)
}

func TestInboundRejectsUnpairedLegacyFunctionResult(t *testing.T) {
	_, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{
		Headers: http.Header{"Content-Type": {"application/json"}},
		Body:    []byte(`{"model":"test","messages":[{"role":"function","name":"missing","content":"orphan"}]}`),
	})
	require.ErrorContains(t, err, "no matching call")
}
