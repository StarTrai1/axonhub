package claudecode

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
)

func TestInjectedSystemCachePreservesLaterOneHourAnchor(t *testing.T) {
	for _, ttl := range []string{"5m", "1h"} {
		t.Run(ttl, func(t *testing.T) {
			body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":32,"messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral","ttl":"` + ttl + `"}}]}]}`)
			inbound := anthropic.NewInboundTransformer()
			req, err := inbound.TransformRequest(t.Context(), &httpclient.Request{Body: body, Headers: http.Header{"Content-Type": {"application/json"}}})
			require.NoError(t, err)
			outbound, err := NewOutboundTransformer(Params{TokenProvider: newMockTokenProvider("synthetic-oauth")})
			require.NoError(t, err)
			result, err := outbound.TransformRequest(t.Context(), req)
			require.NoError(t, err)
			require.Equal(t, ttl, gjson.GetBytes(result.Body, "messages.0.content.0.cache_control.ttl").String())
			if ttl == "1h" {
				require.Equal(t, "1h", gjson.GetBytes(result.Body, "system.0.cache_control.ttl").String())
			} else {
				require.Empty(t, gjson.GetBytes(result.Body, "system.0.cache_control.ttl").String())
			}
			require.Equal(t, body, req.RawRequest.Body)
		})
	}
}
