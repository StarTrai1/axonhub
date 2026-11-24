package gemini

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParameterlessFunctionCallDefaultsToObject(t *testing.T) {
	for _, raw := range []string{`{"name":"ping"}`, `{"name":"ping","args":null}`, `{"name":"ping","args":{}}`} {
		for _, streaming := range []bool{false, true} {
			var upstream GenerateContentResponse
			require.NoError(t, json.Unmarshal([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":`+raw+`,"thoughtSignature":"signed"}]},"finishReason":"STOP"}]}`), &upstream))
			response := convertGeminiToLLMResponse(&upstream, streaming)
			message := response.Choices[0].Message
			if streaming {
				message = response.Choices[0].Delta
			}
			require.Len(t, message.ToolCalls, 1)
			require.Equal(t, "{}", message.ToolCalls[0].Function.Arguments)
			require.NotEmpty(t, message.ToolCalls[0].TransformerMetadata)
			require.Equal(t, "tool_calls", *response.Choices[0].FinishReason)
		}
	}
}
