package anthropic

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestParameterlessAnthropicCallRetainsValidArguments(t *testing.T) {
	for _, tc := range []struct{ input, want string }{{"", "{}"}, {`,"input":{}`, "{}"}, {`,"input":{"value":7}`, `{"value":7}`}} {
		request, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: []byte(`{"model":"claude-test","max_tokens":32,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_ping","name":"ping"`+tc.input+`}]}]}`)})
		require.NoError(t, err)
		require.Len(t, request.Messages, 1)
		require.Equal(t, tc.want, request.Messages[0].ToolCalls[0].Function.Arguments)
	}
}
