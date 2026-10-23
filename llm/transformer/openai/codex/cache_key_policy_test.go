package codex

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/oauth"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestCodex164CacheKeyIndependentOfSession(t *testing.T) {
	for _, base := range []string{"https://chatgpt.com/backend-api/codex", "https://relay.invalid/v1"} {
		for _, present := range []bool{false, true} {
			t.Run(base+"/"+map[bool]string{false:"omit",true:"custom"}[present], func(t *testing.T) {
				body := map[string]any{"model":"gpt-6.1-sol", "input":"hello", "client_metadata":map[string]string{"thread_id":"child-thread", "custom_patched_field":"kept"}}
				if present { body["prompt_cache_key"] = "shared-custom-cache" }
				encoded, err := json.Marshal(body); require.NoError(t, err)
				raw := &httpclient.Request{Body:encoded, Headers:http.Header{SessionHeaderHyphen:{"independent-session"}, "User-Agent":{"codex_cli_rs/0.164.0"}, "Originator":{"codex_cli_rs"}}}
				req, err := responses.NewInboundTransformer().TransformRequest(t.Context(), raw); require.NoError(t, err); req.RawRequest = raw
				outbound, err := NewOutboundTransformer(Params{BaseURL:base, TokenProvider:staticTokenGetter{creds:&oauth.OAuthCredentials{AccessToken:testAccessTokenWithAccountID(t), ExpiresAt:time.Now().Add(time.Hour)}}}); require.NoError(t, err)
				wire, err := outbound.TransformRequest(t.Context(), req); require.NoError(t, err)
				require.Equal(t, "independent-session", wire.Headers.Get(SessionHeaderHyphen))
				require.Equal(t, present, gjson.GetBytes(wire.Body, "prompt_cache_key").Exists())
				if present { require.Equal(t, "shared-custom-cache", gjson.GetBytes(wire.Body, "prompt_cache_key").String()) }
				require.Equal(t, "kept", gjson.GetBytes(wire.Body, "client_metadata.custom_patched_field").String())
			})
		}
	}
}
