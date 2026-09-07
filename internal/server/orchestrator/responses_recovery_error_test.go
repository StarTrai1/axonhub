package orchestrator

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestResponsesRecoveryAcceptsStructuredStreamErrors(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		code   string
		param  string
		status int
		want   bool
	}{
		{name: "reasoning", code: "invalid_encrypted_content", status: 400, want: true},
		{name: "indexed reasoning", code: "invalid_encrypted_content", param: "input[1].encrypted_content", status: 400, want: true},
		{name: "metadata", code: "unknown_parameter", param: "input[0].internal_chat_message_metadata_passthrough.content_item_kinds", status: 400, want: true},
		{name: "unrelated parameter", code: "unknown_parameter", param: "input[0].content", status: 400},
		{name: "server error", code: "invalid_encrypted_content", status: 500},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			outbound := newCodexResponsesPassThroughOutbound()
			outbound.state.RawProviderRequest.Body = []byte(responsesRejectedReasoningFixture)
			failure := &llm.ResponseError{StatusCode: scenario.status, Detail: llm.ErrorDetail{Code: scenario.code, Param: scenario.param, Type: "invalid_request_error"}}
			applyResponsesRejectedStatusCompatibility(outbound).OnOutboundRawError(t.Context(), failure)
			require.Equal(t, scenario.want, hasResponsesRejectedStatusCompatibilityRetry(outbound.state, outbound.state.CurrentCandidate.Channel.ID))
		})
	}
}

func TestGenericProviderErrorsRetainStructuredReason(t *testing.T) {
	for _, failure := range []error{
		&httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"message":"bad response status code 400","code":"unknown_parameter","param":"input[0].internal_chat_message_metadata_passthrough"}}`)},
		&llm.ResponseError{StatusCode: http.StatusBadRequest, Detail: llm.ErrorDetail{Message: "bad response status code 400", Code: "unknown_parameter", Param: "input[0].internal_chat_message_metadata_passthrough"}},
	} {
		require.Equal(t, "bad response status code 400 [code=unknown_parameter] [param=input[0].internal_chat_message_metadata_passthrough]", ExtractErrorMessage(failure))
	}
	require.Equal(t, "specific upstream explanation", enrichGenericProviderError("specific upstream explanation", "code", "param"))
}

type continuationHTTPFinalizer struct {
	*mockTransformer
}

func (*continuationHTTPFinalizer) FinalizeTransportRequest(request *httpclient.Request) *httpclient.Request {
	return responses.PrepareHTTPTransportRequest(request, true)
}

func TestResponsesContinuationNeverDropsUnresolvedHistory(t *testing.T) {
	outbound := &PersistentOutboundTransformer{state: new(PersistenceState), wrapped: &continuationHTTPFinalizer{mockTransformer: new(mockTransformer)}}
	request := &httpclient.Request{Body: []byte(`{"previous_response_id":"resp_missing","input":[{"type":"function_call_output","call_id":"call_native","output":"ok"}]}`)}
	finalized, err := finalizeTransportRequest(outbound).OnOutboundRawRequest(context.Background(), request)
	require.Nil(t, finalized)
	var responseErr *llm.ResponseError
	require.ErrorAs(t, err, &responseErr)
	require.Equal(t, http.StatusBadRequest, responseErr.StatusCode)
	require.Equal(t, "previous_response_not_found", responseErr.Detail.Code)
	require.Contains(t, string(request.Body), "resp_missing")
}
