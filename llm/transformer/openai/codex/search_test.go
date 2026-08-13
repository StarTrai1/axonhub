package codex

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/oauth"
	"github.com/looplj/axonhub/llm/pipeline"
)

func TestCodexSearchOutboundUsesTokenAndIdentityHeaders(t *testing.T) {
	accessToken := testAccessTokenWithAccountID(t)
	outbound, err := NewSearchOutboundTransformer(SearchParams{
		TokenProvider: staticTokenGetter{creds: &oauth.OAuthCredentials{AccessToken: accessToken}},
		BaseURL:       "wss://chatgpt.com/backend-api/codex#",
	})
	require.NoError(t, err)

	req, err := outbound.TransformRequest(t.Context(), &llm.Request{
		Model:       "gpt-5.6-sol",
		RequestType: llm.RequestTypeSearch,
		APIFormat:   llm.APIFormatOpenAISearch,
		RawRequest: &httpclient.Request{Headers: http.Header{
			"Originator":         []string{"codex_cli_rs"},
			"User-Agent":         []string{"codex_cli_rs/0.144.5"},
			TurnMetadataHeader:    []string{`{"session_id":"session-1"}`},
			ClientRequestIDHeader: []string{"request-1"},
		}},
		Search: &llm.SearchRequest{
			Raw: []byte(`{"id":"search-1","model":"gpt-5.6-sol","future":{"keep":true}}`),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://chatgpt.com/backend-api/codex/alpha/search", req.URL)
	require.Equal(t, accessToken, req.Auth.APIKey)
	require.Equal(t, "codex_cli_rs", req.Headers.Get("Originator"))
	require.Equal(t, "codex_cli_rs/0.144.5", req.Headers.Get("User-Agent"))
	require.Equal(t, `{"session_id":"session-1"}`, req.Headers.Get(TurnMetadataHeader))
	require.Equal(t, "request-1", req.Headers.Get(ClientRequestIDHeader))
	require.Equal(t, testChatAccountID, req.Headers.Get("Chatgpt-Account-Id"))
	_, customized := any(outbound).(pipeline.ChannelCustomizedExecutor)
	require.True(t, customized)
}

func TestCodexSearchOutboundSupportsPlainAPIKeyProvider(t *testing.T) {
	tokens := oauth.NewAPIKeyTokenProvider(func(context.Context) string { return "third-party-key" })
	outbound, err := NewSearchOutboundTransformer(SearchParams{
		TokenProvider: tokens,
		BaseURL:       "https://sub.example.test/v1",
	})
	require.NoError(t, err)

	req, err := outbound.TransformRequest(t.Context(), &llm.Request{
		Model:       "gpt-5.6-terra",
		RequestType: llm.RequestTypeSearch,
		APIFormat:   llm.APIFormatOpenAISearch,
		Search: &llm.SearchRequest{
			Raw: []byte(`{"id":"search-2","model":"gpt-5.6-terra"}`),
		},
	})
	require.NoError(t, err)
	require.Equal(t, "https://sub.example.test/v1/alpha/search", req.URL)
	require.Equal(t, "third-party-key", req.Auth.APIKey)
	require.Equal(t, CodexCLIOriginator, req.Headers.Get("Originator"))
	require.True(t, strings.HasPrefix(req.Headers.Get("User-Agent"), CodexCLIOriginator+"/"))
	_, customized := any(outbound).(pipeline.ChannelCustomizedExecutor)
	require.True(t, customized)
}
