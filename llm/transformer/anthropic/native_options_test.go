package anthropic

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestAnthropicNativeOptionsRoundTrip(t *testing.T) {
	for _, thinking := range []string{"between_tools", "adaptive"} {
		t.Run(thinking, func(t *testing.T) {
			request := &httpclient.Request{
				Headers: http.Header{"Content-Type": []string{"application/json"}},
				Body:    []byte(`{"model":"claude-sonnet-5-5","max_tokens":100,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"` + thinking + `","display":"summarized"},"output_config":{"effort":"high"},"safeguards":{"mode":"auto","nested":{"future":9007199254740993}}}`),
			}
			unified, err := NewInboundTransformer().TransformRequest(t.Context(), request)
			require.NoError(t, err)
			got := buildBaseRequest(unified, &Config{Type: PlatformDirect})
			require.Equal(t, thinking, got.Thinking.Type)
			require.Equal(t, "summarized", got.Thinking.Display)
			require.Equal(t, "high", got.OutputConfig.Effort)
			require.JSONEq(t, `{"mode":"auto","nested":{"future":9007199254740993}}`, string(got.Safeguards))
			got.Safeguards[0] = ' '
			require.True(t, json.Valid(unified.TransformerMetadata[transformerMetadataKeySafeguards].(json.RawMessage)))
		})
	}
}

func TestSonnet55ConvertedNoReasoningUsesBetweenTools(t *testing.T) {
	for _, model := range []string{"claude-sonnet-5-5", "claude-sonnet-5"} {
		got := buildBaseRequest(&llm.Request{Model: model, ReasoningEffort: llm.ReasoningEffortNone}, &Config{Type: PlatformDirect})
		if model == "claude-sonnet-5-5" {
			require.Equal(t, "between_tools", got.Thinking.Type)
			require.Equal(t, "low", got.OutputConfig.Effort)
		} else {
			require.Equal(t, "disabled", got.Thinking.Type)
		}
	}
}
