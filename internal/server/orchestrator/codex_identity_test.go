package orchestrator

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/oauth"
)

func codexIdentityTestOutbound(policy objects.CodexIdentityPolicy, oauthCredentials bool) *PersistentOutboundTransformer {
	credentials := objects.ChannelCredentials{APIKeys: []string{"third-party-key"}}
	if oauthCredentials {
		credentials = objects.ChannelCredentials{OAuth: &oauth.OAuthCredentials{AccessToken: "oauth-token"}}
	}

	outbound := newTestOutbound(&biz.Channel{Channel: &ent.Channel{
		ID:          42,
		Type:        channel.TypeCodex,
		Credentials: credentials,
		Policies:    objects.ChannelPolicies{CodexIdentity: policy},
	}})
	outbound.state.LlmRequest = &llm.Request{RawRequest: &httpclient.Request{Headers: make(http.Header)}}

	return outbound
}

func TestApplyCodexIdentityPolicy_NoOpForOffAndAPIKeyChannels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		policy   objects.CodexIdentityPolicy
		oauth    bool
	}{
		{name: "explicit off", policy: objects.CodexIdentityPolicyOff, oauth: true},
		{name: "API key channel", policy: objects.CodexIdentityPolicyFull, oauth: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			originalBody := []byte(`{"model":"gpt-5.6-sol","input":[]}`)
			request := &httpclient.Request{
				Headers: http.Header{
					"Chatgpt-Account-Id":       []string{"acct-1"},
					codexInstallationIDHeader: []string{"client-installation"},
					codexSessionIDHeader:      []string{"client-session"},
				},
				Body: append([]byte(nil), originalBody...),
			}

			processed, err := applyCodexIdentityPolicy(codexIdentityTestOutbound(test.policy, test.oauth)).
				OnOutboundRawRequest(t.Context(), request)
			require.NoError(t, err)
			require.Equal(t, "client-installation", processed.Headers.Get(codexInstallationIDHeader))
			require.Equal(t, "client-session", processed.Headers.Get(codexSessionIDHeader))
			require.Equal(t, originalBody, processed.Body)
		})
	}
}

func TestApplyCodexIdentityPolicy_DeviceOnlyChangesInstallationIdentity(t *testing.T) {
	t.Parallel()

	request := &httpclient.Request{
		Headers: http.Header{
			"Chatgpt-Account-Id":       []string{"acct-1"},
			codexInstallationIDHeader: []string{"client-installation"},
			codexSessionIDHeader:      []string{"client-session"},
			codexWindowIDHeader:       []string{"client-thread:3"},
			codexTurnMetadataHeader:   []string{`{"installation_id":"client-installation","session_id":"client-session","sandbox":"workspace-write"}`},
		},
		Body: []byte(`{"model":"gpt-5.6-sol","client_metadata":{"x-codex-installation-id":"client-installation","session_id":"client-session","custom":"keep"}}`),
	}

	processed, err := applyCodexIdentityPolicy(codexIdentityTestOutbound(objects.CodexIdentityPolicyDevice, true)).
		OnOutboundRawRequest(t.Context(), request)
	require.NoError(t, err)
	require.NotEqual(t, "client-installation", processed.Headers.Get(codexInstallationIDHeader))
	require.Equal(t, "client-session", processed.Headers.Get(codexSessionIDHeader))
	require.Equal(t, "client-thread:3", processed.Headers.Get(codexWindowIDHeader))

	var metadata map[string]any
	require.NoError(t, json.Unmarshal([]byte(processed.Headers.Get(codexTurnMetadataHeader)), &metadata))
	require.Equal(t, processed.Headers.Get(codexInstallationIDHeader), metadata["installation_id"])
	require.Equal(t, "client-session", metadata["session_id"])
	require.Equal(t, "workspace-write", metadata["sandbox"])
	require.Equal(t, "client-session", jsonBodyString(t, processed.Body, "client_metadata", "session_id"))
	require.Equal(t, "keep", jsonBodyString(t, processed.Body, "client_metadata", "custom"))
}

func TestApplyCodexIdentityPolicy_SessionKeepsAllCarriersCoherent(t *testing.T) {
	t.Parallel()

	request := &httpclient.Request{
		Headers: http.Header{
			"Chatgpt-Account-Id":     []string{"acct-1"},
			codexSessionIDHeader:    []string{"client-session"},
			codexTurnMetadataHeader: []string{`{"session_id":"client-session","thread_id":"client-thread","turn_id":"client-turn","sandbox":"danger-full-access","thread_source":"cli"}`},
		},
		Body: []byte(`{"model":"gpt-5.6-sol","client_metadata":{"session_id":"client-session","thread_id":"client-thread","x-codex-turn-metadata":"{\"session_id\":\"client-session\",\"sandbox\":\"danger-full-access\"}","custom":"keep"}}`),
	}

	outbound := codexIdentityTestOutbound(objects.CodexIdentityPolicySession, true)
	outbound.state.LlmRequest.RawRequest.Headers.Set(codexSessionIDHeader, "client-session")
	processed, err := applyCodexIdentityPolicy(outbound).
		OnOutboundRawRequest(t.Context(), request)
	require.NoError(t, err)

	sessionID := processed.Headers.Get(codexSessionIDHeader)
	threadID := processed.Headers.Get(codexThreadIDHeader)
	require.NotEmpty(t, sessionID)
	require.NotEmpty(t, threadID)
	require.NotEqual(t, sessionID, threadID)
	require.Equal(t, threadID+":0", processed.Headers.Get(codexWindowIDHeader))
	require.Equal(t, threadID, processed.Headers.Get(codexClientRequestIDHeader))

	var headerMetadata map[string]any
	require.NoError(t, json.Unmarshal([]byte(processed.Headers.Get(codexTurnMetadataHeader)), &headerMetadata))
	require.Equal(t, sessionID, headerMetadata["session_id"])
	require.Equal(t, threadID, headerMetadata["thread_id"])
	require.Equal(t, "danger-full-access", headerMetadata["sandbox"])
	require.Equal(t, "cli", headerMetadata["thread_source"])

	require.Equal(t, sessionID, jsonBodyString(t, processed.Body, "client_metadata", "session_id"))
	require.Equal(t, threadID, jsonBodyString(t, processed.Body, "client_metadata", "thread_id"))
	require.Equal(t, headerMetadata["turn_id"], jsonBodyString(t, processed.Body, "client_metadata", "turn_id"))
	require.Equal(t, "keep", jsonBodyString(t, processed.Body, "client_metadata", "custom"))
}

func TestResolveCodexIdentityValues_FullConvergesThreadToSession(t *testing.T) {
	t.Parallel()

	first := resolveCodexIdentityValues(42, "acct-1", "client-a", "thread-a", objects.CodexIdentityPolicyFull)
	second := resolveCodexIdentityValues(42, "acct-1", "client-b", "thread-b", objects.CodexIdentityPolicyFull)
	require.NotNil(t, first)
	require.NotNil(t, second)
	require.Equal(t, first.sessionID, first.threadID)
	require.Equal(t, first.threadID, second.threadID)
	require.Equal(t, first.installationID, second.installationID)
	require.NotEqual(t, first.turnID, second.turnID)
}

func TestResolveCodexIdentityValues_UsesChannelFallbackWithoutAccountHeader(t *testing.T) {
	t.Parallel()

	first := resolveCodexIdentityValues(42, "", "client-session", "", objects.CodexIdentityPolicySession)
	second := resolveCodexIdentityValues(42, "", "client-session", "", objects.CodexIdentityPolicySession)
	require.NotNil(t, first)
	require.NotNil(t, second)
	require.Equal(t, first.installationID, second.installationID)
	require.Equal(t, first.sessionID, second.sessionID)
	require.Equal(t, first.threadID, second.threadID)
}

func TestApplyCodexIdentityBody_DoesNotCreateMissingClientMetadata(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"gpt-5.6-sol","input":[]}`)
	values := resolveCodexIdentityValues(42, "acct-1", "client-session", "", objects.CodexIdentityPolicySession)
	processed, err := applyCodexIdentityBody(body, values)
	require.NoError(t, err)
	require.Equal(t, body, processed)
}

func TestOriginalCodexSessionID_DoesNotUseGeneratedProviderSession(t *testing.T) {
	t.Parallel()

	outbound := codexIdentityTestOutbound(objects.CodexIdentityPolicySession, true)
	require.Empty(t, originalCodexSessionID(outbound))
	outbound.state.LlmRequest.RawRequest.Headers.Set("Session_id", "legacy-client-session")
	require.Equal(t, "legacy-client-session", originalCodexSessionID(outbound))
}

func jsonBodyString(t *testing.T, body []byte, path ...string) string {
	t.Helper()

	var value any
	require.NoError(t, json.Unmarshal(body, &value))
	current := value
	for _, key := range path {
		object, ok := current.(map[string]any)
		require.True(t, ok)
		current = object[key]
	}
	result, ok := current.(string)
	require.True(t, ok)

	return result
}
