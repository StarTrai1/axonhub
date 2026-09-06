package orchestrator

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestResponsesRejectedCodexMetadataRetriesWithoutChangingContent(t *testing.T) {
	body := []byte(`{"model":"gpt-6-astra","input":[{"type":"message","role":"developer","content":[{"type":"input_text","text":"rules"}],"status":"completed","internal_chat_message_metadata_passthrough":{"content_item_kinds":["permissions.instructions"],"turn_id":"turn"}},{"type":"message","role":"user","content":"hello","internal_chat_message_metadata_passthrough":{"turn_id":"turn"}},{"type":"compaction","encrypted_content":"opaque"}],"tools":[{"type":"function","name":"run"}],"prompt_cache_key":"stable"}`)
	channel := new(biz.Channel)
	channel.Channel = new(ent.Channel)
	channel.ID = 29
	candidate := new(ChannelModelsCandidate)
	candidate.Channel = channel
	state := new(PersistenceState)
	state.CurrentCandidate = candidate
	state.RawProviderRequest = new(httpclient.Request)
	state.RawProviderRequest.APIFormat = string(llm.APIFormatOpenAIResponse)
	state.RawProviderRequest.Body = body
	outbound := new(PersistentOutboundTransformer)
	outbound.state = state
	middleware := applyResponsesRejectedStatusCompatibility(outbound)
	providerErr := new(httpclient.Error)
	providerErr.StatusCode = http.StatusBadRequest
	providerErr.Body = []byte(`{"error":{"message":"bad response status code 400","param":"input[0].internal_chat_message_metadata_passthrough.content_item_kinds","code":"unknown_parameter"}}`)

	middleware.OnOutboundRawError(t.Context(), providerErr)
	require.True(t, outbound.CanRetry(providerErr))
	require.NoError(t, outbound.PrepareForRetry(t.Context()))
	request := new(httpclient.Request)
	request.Body = body
	retry, err := middleware.OnOutboundRawRequest(t.Context(), request)
	require.NoError(t, err)
	for _, path := range []string{"input.0.internal_chat_message_metadata_passthrough", "input.1.internal_chat_message_metadata_passthrough"} {
		require.False(t, gjson.GetBytes(retry.Body, path).Exists())
		require.True(t, gjson.GetBytes(body, path).Exists())
	}
	for _, path := range []string{"input.0.content", "input.0.status", "input.1.content", "input.2", "tools", "prompt_cache_key"} {
		require.Equal(t, gjson.GetBytes(body, path).Raw, gjson.GetBytes(retry.Body, path).Raw)
	}
	middleware.OnOutboundRawError(t.Context(), providerErr)
	require.False(t, outbound.CanRetry(providerErr))

	channel.ID = 30
	request.Body = body
	untouched, err := middleware.OnOutboundRawRequest(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, body, untouched.Body)
}

func TestResponsesRejectedMetadataRejectsSemanticAndUnrelatedErrors(t *testing.T) {
	body := []byte(`{"input":[{"type":"message","content":"keep","status":"completed","internal_chat_message_metadata_passthrough":{"turn_id":"turn"}}]}`)
	for _, param := range []string{"input[0].content", "input[0].status.nested", "input[1].internal_chat_message_metadata_passthrough", "input[0].internal_chat_message_metadata_passthrough.content[0]"} {
		t.Run(param, func(t *testing.T) {
			providerErr := new(httpclient.Error)
			providerErr.StatusCode = http.StatusBadRequest
			providerErr.Body = []byte(`{"error":{"code":"unknown_parameter","param":"` + param + `"}}`)
			_, ok := responsesRejectedStatusRuleFromError(providerErr, body)
			require.False(t, ok)
		})
	}
}

func TestResponsesRejectedMetadataLearningIsScopedAndExpires(t *testing.T) {
	body := []byte(`{"model":"gpt-6-astra","input":[{"type":"message","content":"keep","internal_chat_message_metadata_passthrough":{"turn_id":"turn"}}]}`)
	request := &httpclient.Request{
		URL: "https://metadata-compatibility.example/responses", APIFormat: string(llm.APIFormatOpenAIResponse),
		Headers: http.Header{"Authorization": {"Bearer credential-a"}}, Body: body,
	}
	key := responsesMetadataKey(929, request)
	rejectedResponsesMetadata.Add(key, time.Now().Add(time.Minute))
	t.Cleanup(func() { rejectedResponsesMetadata.Remove(key) })
	for _, variant := range []struct {
		name       string
		channelID  int
		credential string
		url        string
		expired    bool
		wantStrip  bool
	}{
		{name: "learned", channelID: 929, credential: "credential-a", url: request.URL, wantStrip: true},
		{name: "other_channel", channelID: 930, credential: "credential-a", url: request.URL},
		{name: "other_credential", channelID: 929, credential: "credential-b", url: request.URL},
		{name: "other_endpoint", channelID: 929, credential: "credential-a", url: "https://other.example/responses"},
		{name: "expired", channelID: 929, credential: "credential-a", url: request.URL, expired: true},
	} {
		t.Run(variant.name, func(t *testing.T) {
			if variant.expired {
				rejectedResponsesMetadata.Add(key, time.Now().Add(-time.Second))
			}
			state := &PersistenceState{CurrentCandidate: &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: variant.channelID}}}}
			outbound := &PersistentOutboundTransformer{state: state}
			outgoing := *request
			outgoing.URL = variant.url
			outgoing.Headers = http.Header{"Authorization": {"Bearer " + variant.credential}}
			result, err := applyResponsesRejectedStatusCompatibility(outbound).OnOutboundRawRequest(t.Context(), &outgoing)
			require.NoError(t, err)
			require.Equal(t, !variant.wantStrip, gjson.GetBytes(result.Body, "input.0.internal_chat_message_metadata_passthrough").Exists())
			require.Equal(t, "keep", gjson.GetBytes(result.Body, "input.0.content").String())
		})
	}
}
