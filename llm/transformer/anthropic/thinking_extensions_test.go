package anthropic

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/auth"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestThinkingExtensionsSurviveNativeTranslation(t *testing.T) {
	for _, platform := range []PlatformType{PlatformDirect, PlatformBedrock} {
		t.Run(string(platform), func(t *testing.T) {
			const thinking = `{"type":"adaptive","display":"summarized","block_binding":{"prefix_mismatch_behavior":"drop_block"},"future_field":{"count":9007199254740993}}`
			raw := &httpclient.Request{Headers: http.Header{"Content-Type": {"application/json"}, "Anthropic-Beta": {"fast-mode-2026-02-01", " thinking-binding-controls-2026-08-01, "}}, Body: []byte(`{"model":"claude-opus-5-5","max_tokens":128,"messages":[{"role":"user","content":"hello"}],"thinking":`+thinking+`}`)}
			request, err := NewInboundTransformer().TransformRequest(t.Context(), raw)
			require.NoError(t, err)
			request.RawRequest = raw
			outbound, err := NewOutboundTransformerWithConfig(&Config{Type: platform, BaseURL: "https://example.com", Region: "us-east-1", APIKeyProvider: auth.NewStaticKeyProvider("test-key")})
			require.NoError(t, err)
			wire, err := outbound.TransformRequest(t.Context(), request)
			require.NoError(t, err)
			var body map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(wire.Body, &body))
			require.JSONEq(t, thinking, string(body["thinking"]))
			if platform == PlatformBedrock {
				require.JSONEq(t, `["fast-mode-2026-02-01","thinking-binding-controls-2026-08-01"]`, string(body["anthropic_beta"]))
			} else {
				merged := httpclient.MergeInboundRequest(wire, raw)
				require.Contains(t, merged.Headers.Get("Anthropic-Beta"), "fast-mode-2026-02-01")
				require.Contains(t, merged.Headers.Get("Anthropic-Beta"), "thinking-binding-controls-2026-08-01")
			}
			require.Len(t, raw.Headers.Values("Anthropic-Beta"), 2)
		})
	}
}

func TestThinkingExtensionsDoNotOverrideKnownFields(t *testing.T) {
	thinking := Thinking{Type: "adaptive", Extra: map[string]json.RawMessage{"type": json.RawMessage(`"enabled"`), "budget_tokens": json.RawMessage(`4096`)}}
	raw, err := json.Marshal(thinking)
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"adaptive"}`, string(raw))
}
