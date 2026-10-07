package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestOpus55DefaultEffortAcrossRequestForms(t *testing.T) {
	for _, tc := range []struct{ model, thinking, effort, want string }{
		{"claude-opus-5-5", "", "", "medium"},
		{"claude-opus-5-5-20261001", "adaptive", "", "medium"},
		{"claude-opus-5-5", "adaptive", "high", "high"},
		{"claude-opus-5-5", "adaptive", "max", "max"},
		{"claude-opus-5", "adaptive", "", "high"},
		{"claude-sonnet-5-5", "adaptive", "", "high"},
		{"claude-opus-5-50", "adaptive", "", "high"},
		{"claude-opus-5-5", "disabled", "", "none"},
	} {
		t.Run(tc.model+"/"+tc.thinking+"/"+tc.effort, func(t *testing.T) {
			payload := map[string]any{"model": tc.model, "max_tokens": 1000, "messages": []any{map[string]any{"role": "user", "content": "hello"}}}
			if tc.thinking != "" {
				payload["thinking"] = map[string]any{"type": tc.thinking}
			}
			if tc.effort != "" {
				payload["output_config"] = map[string]any{"effort": tc.effort}
			}
			body, err := json.Marshal(payload)
			require.NoError(t, err)
			request, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: body})
			require.NoError(t, err)
			require.Equal(t, tc.want, request.ReasoningEffort)
		})
	}
}
