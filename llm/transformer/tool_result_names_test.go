package transformer_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/openai"
)

func TestAnthropicToolResultsKeepMatchingChatNames(t *testing.T) {
	input := []byte(`{"model":"test","max_tokens":100,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_a","name":"lookup","input":{"id":1}},{"type":"tool_use","id":"call_b","name":"read","input":{"id":2}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_b","content":"second result"},{"type":"tool_result","tool_use_id":"call_a","content":"first result"}]}]}`)
	req, err := anthropic.NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: input, ContentType: "application/json", Headers: http.Header{"Content-Type": {"application/json"}}})
	require.NoError(t, err)
	before, err := json.Marshal(req)
	require.NoError(t, err)
	chat := openai.RequestFromLLM(t.Context(), req, openai.ReasoningFieldContent)
	results := make(map[string]string)
	for _, message := range chat.Messages {
		if message.Role == "tool" {
			require.NotNil(t, message.Name)
			results[*message.ToolCallID] = *message.Name
		}
	}
	require.Equal(t, map[string]string{"call_a": "lookup", "call_b": "read"}, results)
	after, err := json.Marshal(req)
	require.NoError(t, err)
	require.Equal(t, before, after)
	req.APIFormat = llm.APIFormatOpenAIChatCompletion
	native := openai.RequestFromLLM(t.Context(), req, openai.ReasoningFieldContent)
	for _, message := range native.Messages {
		if message.Role == "tool" {
			require.Nil(t, message.Name, "native Chat fields are not inferred")
		}
	}
}
