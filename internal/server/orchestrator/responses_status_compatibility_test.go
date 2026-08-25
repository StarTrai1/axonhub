package orchestrator

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestResponsesRejectedStatusCompatibilityClearsWholeItemType(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"tool_search_output","status":"completed","call_id":"call_0","tools":[]},
			{"type":"tool_search_output","status":"completed","call_id":"call_1","tools":[]},
			{"type":"message","role":"user","status":"completed","content":"continue"}
		]
	}`)
	candidate := &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{
		ID:   7,
		Name: "responses-relay",
	}}}
	state := &PersistenceState{
		CurrentCandidate: candidate,
		RawProviderRequest: &httpclient.Request{
			APIFormat: string(llm.APIFormatOpenAIResponse),
			Body:      append([]byte(nil), body...),
		},
	}
	outbound := &PersistentOutboundTransformer{state: state}
	middleware := applyResponsesRejectedStatusCompatibility(outbound)
	providerErr := &httpclient.Error{
		StatusCode: http.StatusBadRequest,
		Body: []byte(`{
			"error":{
				"code":"unknown_parameter",
				"message":"Unknown parameter: 'input[1].status'.",
				"param":"input[1].status"
			}
		}`),
	}

	middleware.OnOutboundRawError(context.Background(), providerErr)
	require.True(t, outbound.CanRetry(providerErr))
	require.NoError(t, outbound.PrepareForRetry(context.Background()))
	require.False(t, outbound.CanRetry(providerErr))

	retry, err := middleware.OnOutboundRawRequest(context.Background(), &httpclient.Request{Body: append([]byte(nil), body...)})
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(retry.Body, "input.0.status").Exists())
	require.False(t, gjson.GetBytes(retry.Body, "input.1.status").Exists())
	require.Equal(t, "completed", gjson.GetBytes(retry.Body, "input.2.status").String())
	require.Equal(t, "call_0", gjson.GetBytes(retry.Body, "input.0.call_id").String())
}

func TestResponsesRejectedStatusCompatibilityClearsOnlyUntypedIndex(t *testing.T) {
	body := []byte(`{"input":[{"status":"keep"},{"status":"remove"}]}`)
	state := &PersistenceState{
		CurrentCandidate: &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: 8}}},
		RawProviderRequest: &httpclient.Request{
			APIFormat: string(llm.APIFormatOpenAIResponse),
			Body:      append([]byte(nil), body...),
		},
	}
	middleware := applyResponsesRejectedStatusCompatibility(&PersistentOutboundTransformer{state: state})
	middleware.OnOutboundRawError(context.Background(), &httpclient.Error{
		StatusCode: http.StatusBadRequest,
		Body:       []byte(`{"error":{"code":"unsupported_parameter","param":"input[1].status"}}`),
	})

	retry, err := middleware.OnOutboundRawRequest(context.Background(), &httpclient.Request{Body: append([]byte(nil), body...)})
	require.NoError(t, err)
	require.Equal(t, "keep", gjson.GetBytes(retry.Body, "input.0.status").String())
	require.False(t, gjson.GetBytes(retry.Body, "input.1.status").Exists())
}

func TestResponsesRejectedStatusCompatibilityIgnoresUnrelatedBadRequest(t *testing.T) {
	body := []byte(`{"input":[{"type":"message","status":"completed"}]}`)
	state := &PersistenceState{
		CurrentCandidate: &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: 9}}},
		RawProviderRequest: &httpclient.Request{
			APIFormat: string(llm.APIFormatOpenAIResponse),
			Body:      body,
		},
	}
	outbound := &PersistentOutboundTransformer{state: state}
	applyResponsesRejectedStatusCompatibility(outbound).OnOutboundRawError(context.Background(), &httpclient.Error{
		StatusCode: http.StatusBadRequest,
		Body:       []byte(`{"error":{"code":"invalid_request_error","message":"invalid model"}}`),
	})

	require.False(t, hasResponsesRejectedStatusCompatibilityRetry(state, 9))
	require.Empty(t, state.responsesRejectedStatusRules)
}

func TestResponsesRejectedStatusCompatibilityDoesNotDisableInjectedWebSearch(t *testing.T) {
	body := []byte(`{"input":[{"type":"tool_search_output","status":"completed"}]}`)
	candidate := &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: 10, Name: "lite-relay"}}}
	state := &PersistenceState{
		CurrentCandidate: candidate,
		RawProviderRequest: &httpclient.Request{
			APIFormat: string(llm.APIFormatOpenAIResponse),
			Body:      body,
		},
		responsesLiteWebSearchInjectedChannel: 10,
	}
	outbound := &PersistentOutboundTransformer{state: state}
	providerErr := &httpclient.Error{
		StatusCode: http.StatusBadRequest,
		Body:       []byte(`{"error":{"code":"unknown_parameter","param":"input[0].status"}}`),
	}

	applyResponsesRejectedStatusCompatibility(outbound).OnOutboundRawError(context.Background(), providerErr)
	applyResponsesLiteWebSearchFallback(outbound).OnOutboundRawError(context.Background(), providerErr)

	require.True(t, hasResponsesRejectedStatusCompatibilityRetry(state, 10))
	require.False(t, responsesLiteWebSearchBlockedForChannel(state, 10))
}
