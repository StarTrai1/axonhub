package pipeline_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/openai"
)

func TestChatFinishErrorPipelineRetriesOnlyBeforeOutput(t *testing.T) {
	for _, tc := range []struct {
		name            string
		stream, partial bool
		calls           int
	}{
		{"nonstream", false, false, 2}, {"stream before output", true, false, 2}, {"stream after output", true, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			failure := []byte(`{"choices":[{"index":0,"delta":{},"finish_reason":"error"}]}`)
			success := []byte(`{"id":"recovered","choices":[{"index":0,"message":{"role":"assistant","content":"recovered"},"delta":{"role":"assistant","content":"recovered"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`)
			executor := &mockExecutor{
				doFunc: func(_ context.Context, req *httpclient.Request) (*httpclient.Response, error) {
					calls++
					body := success
					if calls == 1 {
						body = failure
					}
					return &httpclient.Response{StatusCode: 200, Body: body, Request: req}, nil
				},
				doStreamFunc: func(_ context.Context, _ *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
					calls++
					if calls > 1 {
						return streams.SliceStream([]*httpclient.StreamEvent{{Data: success}}), nil
					}
					events := []*httpclient.StreamEvent{}
					if tc.partial {
						events = append(events, &httpclient.StreamEvent{Data: []byte(`{"choices":[{"index":0,"delta":{"role":"assistant","content":"partial"}}]}`)})
					}
					events = append(events, &httpclient.StreamEvent{Data: failure})
					return streams.SliceStream(events), nil
				},
			}
			outbound, err := openai.NewOutboundTransformer("https://example.com/v1", "synthetic")
			require.NoError(t, err)
			wrapped := pipeline.NewChannelRetryableWrapper(outbound, 1)
			p := pipeline.NewFactory(executor).Pipeline(anthropic.NewInboundTransformer(), wrapped, pipeline.WithRetry(0, 1, 0))
			result, err := p.Process(t.Context(), &httpclient.Request{Method: http.MethodPost, Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: fmt.Appendf(nil, `{"model":"fixture","max_tokens":128,"stream":%t,"messages":[{"role":"user","content":"inspect"}]}`, tc.stream)})
			require.NoError(t, err)
			if tc.stream {
				defer result.EventStream.Close()
				terminal := false
				for result.EventStream.Next() {
					if result.EventStream.Current().Type == "message_stop" {
						terminal = true
					}
				}
				if tc.partial {
					require.ErrorContains(t, result.EventStream.Err(), `finish_reason "error"`)
					require.False(t, terminal)
				} else {
					require.NoError(t, result.EventStream.Err())
					require.True(t, terminal)
				}
			} else {
				require.Contains(t, string(result.Response.Body), "recovered")
			}
			require.Equal(t, tc.calls, calls)
		})
	}
}
