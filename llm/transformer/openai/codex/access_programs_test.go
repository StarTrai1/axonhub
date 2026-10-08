package codex

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/oauth"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestAccessProgramsSurviveRequestConversion(t *testing.T) {
	apiKey, err := responses.NewOutboundTransformer("https://api.example/v1", "synthetic-key")
	require.NoError(t, err)
	subscription, err := NewOutboundTransformer(Params{
		BaseURL: "https://relay.example/v1",
		TokenProvider: staticTokenGetter{creds: &oauth.OAuthCredentials{AccessToken: testAccessTokenWithAccountID(t)}},
	})
	require.NoError(t, err)
	for _, program := range []string{"", `{}`, `{"cyber":"standard"}`, `{"cyber":"daybreak_blue"}`, `{"cyber":"daybreak_red"}`, `null`, `{"cyber":"invalid-program"}`} {
		for name, inbound := range map[string]transformer.Inbound{
			"create": responses.NewInboundTransformer(), "compact": responses.NewCompactInboundTransformer(),
		} {
			for provider, outbound := range map[string]transformer.Outbound{"api_key": apiKey, "subscription": subscription} {
				t.Run(name+"/"+provider+"/"+program, func(t *testing.T) {
					payload := map[string]any{"model": "gpt-6.1-sol", "input": []any{map[string]any{"role": "user", "content": "hello"}}}
					if program != "" {
						payload["access_programs"] = json.RawMessage(program)
					}
					body, err := json.Marshal(payload)
					require.NoError(t, err)
					request, err := inbound.TransformRequest(t.Context(), &httpclient.Request{Body: body})
					require.NoError(t, err)
					// Exercise rebuilding with no raw-body passthrough, including the clone
					// used by channel retries. Invalid values remain upstream validation errors.
					request.ProviderExtensions = llm.CloneProviderExtensions(request.ProviderExtensions)
					wire, err := outbound.TransformRequest(t.Context(), request)
					require.NoError(t, err)
					require.Equal(t, "gpt-6.1-sol", gjson.GetBytes(wire.Body, "model").String())
					got := gjson.GetBytes(wire.Body, "access_programs")
					if program == "" {
						require.False(t, got.Exists())
					} else {
						require.JSONEq(t, program, got.Raw)
					}
				})
			}
		}
	}
}
