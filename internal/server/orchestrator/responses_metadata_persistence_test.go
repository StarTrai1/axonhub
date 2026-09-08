package orchestrator

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	entchannel "github.com/looplj/axonhub/internal/ent/channel"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func responsesMetadataPersistenceFixture(test *testing.T) (*ent.Client, *httpclient.Request, *PersistentOutboundTransformer) {
	test.Helper()
	client := enttest.NewEntClient(test, "sqlite3", "file:responses_metadata_persistence?mode=memory&_fk=1")
	test.Cleanup(func() { require.NoError(test, client.Close()) })
	request := &httpclient.Request{
		URL:       "https://metadata-persistence.example/" + test.Name() + "/responses",
		APIFormat: string(llm.APIFormatOpenAIResponse),
		Headers: http.Header{
			"Authorization": {"Bearer provider-credential"},
			"Session-Id":    {"client-session"},
			"Thread-Id":     {"client-thread"},
		},
		Auth: &httpclient.AuthConfig{Type: httpclient.AuthTypeBearer, APIKey: "provider-credential"},
		Body: []byte(responsesRejectedReasoningFixture),
	}
	outbound := &PersistentOutboundTransformer{state: &PersistenceState{
		SystemService:      biz.NewSystemService(biz.SystemServiceParams{Ent: client}),
		RawProviderRequest: request,
		LlmRequest:         &llm.Request{Model: "gpt-5.6-sol"},
		CurrentCandidate: &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{
			ID: 1729, Type: entchannel.TypeCodex, Name: "metadata-persistence",
		}}},
	}}
	key := responsesMetadataKey(1729, request)
	test.Cleanup(func() { clearResponsesMetadataTestCache(key) })
	return client, request, outbound
}

func clearResponsesMetadataTestCache(key responsesMetadataCapabilityKey) {
	rejectedResponsesMetadata.Remove(key)
	responsesMetadataLookupMisses.Remove(key)
}

func learnResponsesMetadataForTest(test *testing.T, outbound *PersistentOutboundTransformer) {
	test.Helper()
	applyResponsesRejectedStatusCompatibility(outbound).OnOutboundRawError(test.Context(), &httpclient.Error{
		StatusCode: http.StatusBadRequest,
		Body:       []byte(`{"error":{"message":"bad response status code 400","code":"unknown_parameter","param":"input[0].internal_chat_message_metadata_passthrough.content_item_kinds"}}`),
	})
	require.True(test, hasResponsesRejectedStatusCompatibilityRetry(outbound.state, outbound.GetCurrentChannel().ID))
}

func TestResponsesRejectedMetadataPersistsAcrossRestart(test *testing.T) {
	client, request, outbound := responsesMetadataPersistenceFixture(test)
	learnResponsesMetadataForTest(test, outbound)
	key := responsesMetadataKey(1729, request)
	expiresAt, err := outbound.state.SystemService.LoadResponsesMetadataRejection(test.Context(), key.digest())
	require.NoError(test, err)
	require.True(test, expiresAt.After(time.Now()))
	clearResponsesMetadataTestCache(key)

	restarted := &PersistentOutboundTransformer{state: &PersistenceState{
		SystemService:    biz.NewSystemService(biz.SystemServiceParams{Ent: client}),
		CurrentCandidate: outbound.state.CurrentCandidate,
	}}
	outgoing := *request
	result, err := applyResponsesRejectedStatusCompatibility(restarted).OnOutboundRawRequest(test.Context(), &outgoing)
	require.NoError(test, err)
	expected, err := sjson.DeleteBytes(request.Body, "input.0.internal_chat_message_metadata_passthrough")
	require.NoError(test, err)
	require.JSONEq(test, string(expected), string(result.Body))
	require.True(test, gjson.GetBytes(request.Body, "input.0.internal_chat_message_metadata_passthrough").Exists())
	require.Equal(test, request.Headers, result.Headers)
	require.Equal(test, request.Auth, result.Auth)
	cachedExpiry, found := rejectedResponsesMetadata.Get(key)
	require.True(test, found)
	require.True(test, expiresAt.Equal(cachedExpiry))
	require.False(test, hasResponsesRejectedStatusCompatibilityRetry(restarted.state, 1729))
}

func TestResponsesRejectedMetadataPersistenceKeepsScopeIsolation(test *testing.T) {
	client, request, outbound := responsesMetadataPersistenceFixture(test)
	learnResponsesMetadataForTest(test, outbound)
	clearResponsesMetadataTestCache(responsesMetadataKey(1729, request))

	for _, scenario := range []struct {
		name          string
		channelID     int
		url           string
		model         string
		authorization string
		credential    string
		wantStrip     bool
	}{
		{name: "same_scope", wantStrip: true},
		{name: "other_channel", channelID: 1730},
		{name: "other_endpoint", url: request.URL + "/other"},
		{name: "other_model", model: "gpt-6-astra"},
		{name: "other_header_credential", authorization: "Bearer rotated-header-credential"},
		{name: "other_auth_credential", credential: "rotated-auth-credential"},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			channelID := 1729
			if scenario.channelID != 0 {
				channelID = scenario.channelID
			}
			outgoing := *request
			outgoing.Headers = request.Headers.Clone()
			auth := *request.Auth
			outgoing.Auth = &auth
			if scenario.url != "" {
				outgoing.URL = scenario.url
			}
			if scenario.model != "" {
				var err error
				outgoing.Body, err = sjson.SetBytes(outgoing.Body, "model", scenario.model)
				require.NoError(test, err)
			}
			if scenario.authorization != "" {
				outgoing.Headers.Set("Authorization", scenario.authorization)
			}
			if scenario.credential != "" {
				outgoing.Auth.APIKey = scenario.credential
			}
			key := responsesMetadataKey(channelID, &outgoing)
			clearResponsesMetadataTestCache(key)
			test.Cleanup(func() { clearResponsesMetadataTestCache(key) })
			restarted := &PersistentOutboundTransformer{state: &PersistenceState{
				SystemService: biz.NewSystemService(biz.SystemServiceParams{Ent: client}),
				CurrentCandidate: &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{
					ID: channelID, Type: entchannel.TypeCodex,
				}}},
			}}
			original := string(outgoing.Body)
			result, err := applyResponsesRejectedStatusCompatibility(restarted).OnOutboundRawRequest(test.Context(), &outgoing)
			require.NoError(test, err)
			require.Equal(test, !scenario.wantStrip, gjson.GetBytes(result.Body, "input.0.internal_chat_message_metadata_passthrough").Exists())
			if !scenario.wantStrip {
				require.Equal(test, original, string(result.Body))
			}
		})
	}
}

func TestResponsesRejectedMetadataPersistenceUsesFinalHeaderOverrides(test *testing.T) {
	client, request, outbound := responsesMetadataPersistenceFixture(test)
	originalKey := responsesMetadataKey(1729, request)
	outbound.GetCurrentChannel().Settings = &objects.ChannelSettings{
		OverrideHeaders: []objects.HeaderEntry{{Key: "Authorization", Value: "Bearer overridden-credential"}},
	}
	_, err := applyOverrideRequestHeaders(outbound).OnOutboundRawRequest(test.Context(), request)
	require.NoError(test, err)
	learnResponsesMetadataForTest(test, outbound)
	finalKey := responsesMetadataKey(1729, request)
	require.NotEqual(test, originalKey, finalKey)
	clearResponsesMetadataTestCache(finalKey)
	test.Cleanup(func() { clearResponsesMetadataTestCache(finalKey) })

	restarted := &PersistentOutboundTransformer{state: &PersistenceState{
		SystemService:    biz.NewSystemService(biz.SystemServiceParams{Ent: client}),
		CurrentCandidate: outbound.state.CurrentCandidate,
		LlmRequest:       outbound.state.LlmRequest,
	}}
	outgoing := *request
	outgoing.Headers = request.Headers.Clone()
	outgoing.Headers.Set("Authorization", "Bearer provider-credential")
	_, err = applyOverrideRequestHeaders(restarted).OnOutboundRawRequest(test.Context(), &outgoing)
	require.NoError(test, err)
	result, err := applyResponsesRejectedStatusCompatibility(restarted).OnOutboundRawRequest(test.Context(), &outgoing)
	require.NoError(test, err)
	require.False(test, gjson.GetBytes(result.Body, "input.0.internal_chat_message_metadata_passthrough").Exists())
	missing, err := restarted.state.SystemService.LoadResponsesMetadataRejection(test.Context(), originalKey.digest())
	require.NoError(test, err)
	require.True(test, missing.IsZero())
}

func TestResponsesRejectedMetadataDoesNotLeakAcrossRetryScopes(test *testing.T) {
	client, request, outbound := responsesMetadataPersistenceFixture(test)
	learnResponsesMetadataForTest(test, outbound)
	clearResponsesMetadataTestCache(responsesMetadataKey(1729, request))
	restarted := &PersistentOutboundTransformer{state: &PersistenceState{
		SystemService:    biz.NewSystemService(biz.SystemServiceParams{Ent: client}),
		CurrentCandidate: outbound.state.CurrentCandidate,
	}}
	middleware := applyResponsesRejectedStatusCompatibility(restarted)
	outgoing := *request
	result, err := middleware.OnOutboundRawRequest(test.Context(), &outgoing)
	require.NoError(test, err)
	require.False(test, gjson.GetBytes(result.Body, "input.0.internal_chat_message_metadata_passthrough").Exists())

	for _, scenario := range []string{"credential", "model", "endpoint"} {
		test.Run(scenario, func(test *testing.T) {
			outgoing := *request
			outgoing.Headers = request.Headers.Clone()
			switch scenario {
			case "credential":
				outgoing.Headers.Set("Authorization", "Bearer other-credential")
			case "model":
				outgoing.Body, err = sjson.SetBytes(request.Body, "model", "other-model")
				require.NoError(test, err)
			case "endpoint":
				outgoing.URL += "/other"
			}
			key := responsesMetadataKey(1729, &outgoing)
			test.Cleanup(func() { clearResponsesMetadataTestCache(key) })
			original := string(outgoing.Body)
			result, err := middleware.OnOutboundRawRequest(test.Context(), &outgoing)
			require.NoError(test, err)
			require.Equal(test, original, string(result.Body))
		})
	}
}

func TestResponsesRejectedMetadataPersistenceFailureRetainsBoundedRecovery(test *testing.T) {
	client, request, outbound := responsesMetadataPersistenceFixture(test)
	require.NoError(test, client.Close())
	outgoing := *request
	result, err := applyResponsesRejectedStatusCompatibility(outbound).OnOutboundRawRequest(test.Context(), &outgoing)
	require.NoError(test, err)
	require.Equal(test, request.Body, result.Body)
	learnResponsesMetadataForTest(test, outbound)
	restarted := &PersistentOutboundTransformer{state: &PersistenceState{
		SystemService:    outbound.state.SystemService,
		CurrentCandidate: outbound.state.CurrentCandidate,
	}}
	outgoing = *request
	result, err = applyResponsesRejectedStatusCompatibility(restarted).OnOutboundRawRequest(test.Context(), &outgoing)
	require.NoError(test, err)
	require.False(test, gjson.GetBytes(result.Body, "input.0.internal_chat_message_metadata_passthrough").Exists())
	require.Equal(test, gjson.GetBytes(request.Body, "input.1").Raw, gjson.GetBytes(result.Body, "input.1").Raw)
}

func TestResponsesRejectedMetadataDoesNotPersistGenericOrEncryptedRejections(test *testing.T) {
	for _, errorBody := range []string{
		`{"error":{"message":"bad response status code 400"}}`,
		`{"error":{"code":"unknown_parameter","param":"input[0].content"}}`,
		`{"error":{"code":"invalid_encrypted_content"}}`,
	} {
		test.Run(errorBody, func(test *testing.T) {
			client, request, outbound := responsesMetadataPersistenceFixture(test)
			applyResponsesRejectedStatusCompatibility(outbound).OnOutboundRawError(test.Context(), &httpclient.Error{
				StatusCode: http.StatusBadRequest, Body: []byte(errorBody),
			})
			count, err := client.System.Query().Count(authz.WithTestBypass(test.Context()))
			require.NoError(test, err)
			require.Zero(test, count)
			restarted := &PersistentOutboundTransformer{state: &PersistenceState{
				SystemService:    biz.NewSystemService(biz.SystemServiceParams{Ent: client}),
				CurrentCandidate: outbound.state.CurrentCandidate,
			}}
			outgoing := *request
			result, err := applyResponsesRejectedStatusCompatibility(restarted).OnOutboundRawRequest(test.Context(), &outgoing)
			require.NoError(test, err)
			require.Equal(test, request.Body, result.Body)
		})
	}
}

func TestResponsesRejectedMetadataConcurrentColdRestore(test *testing.T) {
	client, request, outbound := responsesMetadataPersistenceFixture(test)
	learnResponsesMetadataForTest(test, outbound)
	clearResponsesMetadataTestCache(responsesMetadataKey(1729, request))
	for range 8 {
		test.Run("restore", func(test *testing.T) {
			test.Parallel()
			restarted := &PersistentOutboundTransformer{state: &PersistenceState{
				SystemService:    biz.NewSystemService(biz.SystemServiceParams{Ent: client}),
				CurrentCandidate: outbound.state.CurrentCandidate,
			}}
			outgoing := *request
			result, err := applyResponsesRejectedStatusCompatibility(restarted).OnOutboundRawRequest(test.Context(), &outgoing)
			require.NoError(test, err)
			require.False(test, gjson.GetBytes(result.Body, "input.0.internal_chat_message_metadata_passthrough").Exists())
		})
	}
}
