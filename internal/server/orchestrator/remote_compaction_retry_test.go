package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestResponsesRejectedMetadataLocalCompactionRetry(t *testing.T) {
	for index, scenario := range []struct {
		name       string
		enabled    bool
		maxRetries int
		errorBody  string
		wantCalls  int
	}{
		{"compatible retry", true, 1, `{"error":{"code":"unknown_parameter","param":"input[0].internal_chat_message_metadata_passthrough.content_item_kinds"}}`, 2},
		{"disabled retry", false, 3, `{"error":{"code":"unknown_parameter","param":"input[0].internal_chat_message_metadata_passthrough.content_item_kinds"}}`, 1},
		{"zero retry budget", true, 0, `{"error":{"code":"unknown_parameter","param":"input[0].internal_chat_message_metadata_passthrough.content_item_kinds"}}`, 1},
		{"unrelated validation error", true, 3, `{"error":{"code":"invalid_responses_request","param":"input"}}`, 1},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			outbound := newCodexResponsesPassThroughOutbound()
			outbound.state.CurrentCandidate.Channel.ID = 91000 + index
			outbound.state.RetryPolicyProvider = &mockRetryPolicyProvider{policy: &biz.RetryPolicy{
				Enabled: scenario.enabled, MaxSingleChannelRetries: scenario.maxRetries,
			}}
			providerRequest := &httpclient.Request{
				URL:       "https://example.invalid/v1/responses",
				APIFormat: string(llm.APIFormatOpenAIResponse),
				Headers:   make(http.Header),
				Body:      []byte(`{"model":"gpt-6-astra","input":[{"type":"message","role":"user","content":"preserve context","internal_chat_message_metadata_passthrough":{"content_item_kinds":[]}},{"type":"reasoning","id":"rs_native","encrypted_content":"keep-reasoning"},{"type":"compaction","id":"cmp_native","encrypted_content":"keep-compaction"},{"type":"function_call","id":"fc_native","call_id":"call_native","name":"exec","arguments":"{}"}]}`),
			}
			executor := &localCompactionRetryExecutor{failure: &httpclient.Error{
				StatusCode: http.StatusBadRequest, Body: []byte(scenario.errorBody),
			}}
			adapter := newRemoteCompactionAdapter(nil, nil, nil)
			stream, execution, err := adapter.startLocalCompactionStream(context.Background(), outbound, providerRequest, executor, applyResponsesRejectedStatusCompatibility(outbound))
			require.Nil(t, execution)
			require.Len(t, executor.bodies, scenario.wantCalls)
			if scenario.wantCalls == 1 {
				require.ErrorIs(t, err, executor.failure)
				require.Nil(t, stream)
				return
			}
			require.NoError(t, err)
			require.NoError(t, stream.Close())
			require.True(t, gjson.GetBytes(executor.bodies[0], "input.0.internal_chat_message_metadata_passthrough").Exists())
			require.False(t, gjson.GetBytes(executor.bodies[1], "input.0.internal_chat_message_metadata_passthrough").Exists())
			require.Equal(t, "preserve context", gjson.GetBytes(executor.bodies[1], "input.0.content").String())
			require.Equal(t, "keep-reasoning", gjson.GetBytes(executor.bodies[1], "input.1.encrypted_content").String())
			require.Equal(t, "keep-compaction", gjson.GetBytes(executor.bodies[1], "input.2.encrypted_content").String())
			require.Equal(t, "fc_native", gjson.GetBytes(executor.bodies[1], "input.3.id").String())
		})
	}
}

func TestResponsesRejectedMetadataLocalCompactionHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outbound := newCodexResponsesPassThroughOutbound()
	executor := new(localCompactionRetryExecutor)
	adapter := newRemoteCompactionAdapter(nil, nil, nil)
	stream, execution, err := adapter.startLocalCompactionStream(ctx, outbound, outbound.state.RawProviderRequest, executor, applyResponsesRejectedStatusCompatibility(outbound))
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, stream)
	require.Nil(t, execution)
	require.Empty(t, executor.bodies)
}

type localCompactionRetryExecutor struct {
	bodies  [][]byte
	failure error
}

func (executor *localCompactionRetryExecutor) Do(context.Context, *httpclient.Request) (*httpclient.Response, error) {
	return nil, errors.New("unexpected non-streaming compaction request")
}

func (executor *localCompactionRetryExecutor) DoStream(_ context.Context, request *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	executor.bodies = append(executor.bodies, append([]byte(nil), request.Body...))
	if len(executor.bodies) == 1 {
		return nil, executor.failure
	}
	return streams.SliceStream([]*httpclient.StreamEvent{}), nil
}
