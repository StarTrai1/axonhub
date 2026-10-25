package pipeline_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

type retryEmptyResponsesOutbound struct { transformer.Outbound }

func (*retryEmptyResponsesOutbound) CanRetry(err error) bool { return errors.Is(err, pipeline.ErrEmptyResponse) }
func (*retryEmptyResponsesOutbound) PrepareForRetry(context.Context) error { return nil }

func TestResponsesIntentionalStopsDoNotRetry(t *testing.T) {
	for _, transport := range []string{"http", "sse-incomplete", "sse-completed"} {
		for _, reason := range []string{"interrupted", "steered", "content_filter", "max_messages", "max_output_tokens", "unknown", ""} {
			t.Run(transport+"/"+reason, func(t *testing.T) {
				status := "incomplete"
				if reason == "" { status = "completed" }
				payload := fmt.Appendf(nil, `{"id":"resp_stop","model":"gpt-test","status":%q,"incomplete_details":{"reason":%q},"output":[],"usage":{"input_tokens":8,"output_tokens":0,"total_tokens":8}}`, status, reason)
				calls := 0
				executor := &mockExecutor{
					doFunc: func(_ context.Context, req *httpclient.Request) (*httpclient.Response, error) {
						calls++
						return &httpclient.Response{StatusCode: http.StatusOK, Body: payload, Request: req}, nil
					},
					doStreamFunc: func(context.Context, *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
						calls++
						eventType := "response.incomplete"
						if transport == "sse-completed" || reason == "" { eventType = "response.completed" }
						return streams.SliceStream([]*httpclient.StreamEvent{{Type: eventType, Data: fmt.Appendf(nil, `{"type":%q,"response":%s}`, eventType, payload)}}), nil
					},
				}
				outbound, err := responses.NewOutboundTransformer("https://example.invalid", "synthetic-key")
				require.NoError(t, err)
				p := pipeline.NewFactory(executor).Pipeline(responses.NewInboundTransformer(), &retryEmptyResponsesOutbound{Outbound: outbound}, pipeline.WithEmptyResponseDetection(), pipeline.WithRetry(0, 1, 0))
				result, err := p.Process(t.Context(), &httpclient.Request{Method: http.MethodPost, Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: fmt.Appendf(nil, `{"model":"gpt-test","input":"hello","stream":%v}`, transport != "http")})
				if reason == "" || reason == "unknown" || reason == "max_output_tokens" {
					require.ErrorIs(t, err, pipeline.ErrEmptyResponse)
					require.Equal(t, 2, calls, "ordinary empty responses must still use the retry budget")
					return
				}
				require.NoError(t, err)
				require.Equal(t, 1, calls, "explicit turn termination must not replay the request")
				terminal := result.Response
				if transport != "http" {
					defer result.EventStream.Close()
					for result.EventStream.Next() {
						event := result.EventStream.Current()
						if gjson.GetBytes(event.Data, "response.status").String() == "incomplete" {
							terminal = &httpclient.Response{Body: []byte(gjson.GetBytes(event.Data, "response").Raw)}
						}
					}
					require.NoError(t, result.EventStream.Err())
				}
				require.NotNil(t, terminal)
				require.Equal(t, "incomplete", gjson.GetBytes(terminal.Body, "status").String())
				require.Equal(t, reason, gjson.GetBytes(terminal.Body, "incomplete_details.reason").String())
				require.Equal(t, int64(8), gjson.GetBytes(terminal.Body, "usage.input_tokens").Int())
			})
		}
	}
}
