package codex

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestCodexFinalizerNormalizesOnlyBlankFunctionArguments(t *testing.T) {
	for _, transport := range []string{responses.TransportHTTP, responses.TransportWebSocket} {
		t.Run(transport, func(t *testing.T) {
			body := []byte(`{"model":"gpt-6-sol","input":[{"type":"function_call","name":"blank","call_id":"c1","arguments":" \n"},{"type":"function_call","name":"invalid","arguments":"not json"},{"type":"function_call","name":"missing"},{"type":"function_call","name":"null","arguments":null},{"type":"custom_tool_call","name":"custom","input":""}],"extra":9007199254740993}`)
			request := &httpclient.Request{Body: body}
			transformer := &OutboundTransformer{transport: transport}
			got := transformer.FinalizeTransportRequest(request)
			require.Equal(t, "{}", gjson.GetBytes(got.Body, "input.0.arguments").String())
			require.Equal(t, "not json", gjson.GetBytes(got.Body, "input.1.arguments").String())
			require.False(t, gjson.GetBytes(got.Body, "input.2.arguments").Exists())
			require.Equal(t, "null", gjson.GetBytes(got.Body, "input.3.arguments").Raw)
			require.Equal(t, "", gjson.GetBytes(got.Body, "input.4.input").String())
			require.Equal(t, "9007199254740993", gjson.GetBytes(got.Body, "extra").Raw)
			require.Equal(t, " \n", gjson.GetBytes(request.Body, "input.0.arguments").String())
		})
	}
}
