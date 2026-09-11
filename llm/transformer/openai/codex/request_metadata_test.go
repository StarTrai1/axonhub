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
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestCodexOutboundPreservesBodyThreadIdentity(t *testing.T) {
	t.Parallel()

	bodyMetadata := `{"session_id":"root-session","thread_id":"body-child","window_id":"body-child:7","request_kind":"memory","extra":{"keep":true}}`
	headerMetadata := `{"session_id":"root-session","thread_id":"header-child","window_id":"header-child:8","request_kind":"turn","extra":{"keep":"header"}}`
	for _, testCase := range []struct {
		name         string
		headers      http.Header
		thread       string
		window       string
		metadata     string
		bodyMetadata string
		invalid      bool
	}{
		{name: "body only", thread: "body-child", window: "body-child:7", metadata: bodyMetadata},
		{name: "invalid body thread header", bodyMetadata: `{"session_id":"root-session","thread_id":"bad\nthread"}`, invalid: true},
		{
			name: "formatted body metadata", thread: "body-child", window: "body-child:7", metadata: bodyMetadata,
			bodyMetadata: "{\n\"session_id\":\"root-session\",\"thread_id\":\"body-child\",\"window_id\":\"body-child:7\",\"request_kind\":\"memory\",\"extra\":{\"keep\":true}\n}",
		},
		{
			name: "header metadata takes precedence",
			headers: http.Header{TurnMetadataHeader: []string{headerMetadata}},
			thread: "header-child", window: "header-child:8", metadata: headerMetadata,
		},
		{
			name: "explicit thread headers take precedence",
			headers: http.Header{
				ThreadIDHeader: []string{"explicit-child"}, WindowIDHeader: []string{"explicit-child:9"},
				TurnMetadataHeader: []string{headerMetadata},
			},
			thread: "explicit-child", window: "explicit-child:9", metadata: headerMetadata,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sourceMetadata := bodyMetadata
			if testCase.bodyMetadata != "" {
				sourceMetadata = testCase.bodyMetadata
			}
			body, err := json.Marshal(map[string]any{
				"model": "gpt-6-astra", "input": "hello",
				"client_metadata": map[string]string{"x-codex-turn-metadata": sourceMetadata},
			})
			require.NoError(t, err)
			raw := &httpclient.Request{Body: body, Headers: testCase.headers.Clone()}
			request, err := responses.NewInboundTransformer().TransformRequest(t.Context(), raw)
			require.NoError(t, err)
			outbound, err := NewOutboundTransformer(Params{
				BaseURL: "https://relay.example/v1",
				TokenProvider: staticTokenGetter{creds: &oauth.OAuthCredentials{
					AccessToken: testAccessTokenWithAccountID(t),
					ExpiresAt:   time.Now().Add(time.Hour),
				}},
			})
			require.NoError(t, err)
			result, err := outbound.TransformRequest(t.Context(), request)
			if testCase.invalid {
				require.ErrorIs(t, err, transformer.ErrInvalidRequest)
				return
			}
			require.NoError(t, err)

			require.Equal(t, "root-session", result.Headers.Get(SessionHeaderHyphen))
			require.Equal(t, testCase.thread, result.Headers.Get(ThreadIDHeader))
			require.Equal(t, testCase.window, result.Headers.Get(WindowIDHeader))
			require.Equal(t, testCase.metadata, result.Headers.Get(TurnMetadataHeader))
			require.Equal(t, sourceMetadata, gjson.GetBytes(result.Body, "client_metadata.x-codex-turn-metadata").String())
		})
	}
}
