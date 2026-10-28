package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestXAIReasoningReplayDropsOnlyNullProtocolContent(t *testing.T) {
	const body = `{"model":"grok","prompt_cache_key":"caller-key","seed":9007199254740993,"input":[{"id":"rs_sealed","type":"reasoning","content":null,"encrypted_content":"opaque-checkpoint","summary":[]},{"type":"reasoning","content":[],"encrypted_content":"other"},{"type":"message","role":"assistant","content":null},{"type":"function_call","call_id":"call_keep","arguments":"{\"content\":null}"}]}`
	for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseWebSocket} {
		for _, endpoint := range []string{"https://api.x.ai/v1/responses", "https://api.openai.com/v1/responses", "https://api.x.ai.example/v1/responses"} {
			request := &httpclient.Request{APIFormat: string(format), URL: endpoint, Body: []byte(body)}
			result, err := applyXAIReasoningContent().OnOutboundRawRequest(t.Context(), request)
			require.NoError(t, err)
			if endpoint != "https://api.x.ai/v1/responses" {
				require.Equal(t, body, string(result.Body))
				continue
			}
			require.False(t, gjson.GetBytes(result.Body, "input.0.content").Exists())
			for _, path := range []string{"input.0.id", "input.0.encrypted_content", "input.0.summary", "input.1", "input.2", "input.3", "prompt_cache_key", "seed"} {
				require.Equal(t, gjson.Get(body, path).Raw, gjson.GetBytes(result.Body, path).Raw, path)
			}
			again, err := applyXAIReasoningContent().OnOutboundRawRequest(t.Context(), result)
			require.NoError(t, err)
			require.Equal(t, result.Body, again.Body)
		}
	}
}
