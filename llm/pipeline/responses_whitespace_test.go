package pipeline_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

// The relay in requests #99929/#99930/#99933 returned completed with a single
// space, no tool output and no usage. A successful empty turn must not escape
// the existing pre-output retry boundary.
func TestResponsesWhitespaceCompletionRetriesBeforeDelivery(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, detectEmpty := range []bool{false, true} {
			if !streaming && !detectEmpty {
				continue
			}
			t.Run(fmt.Sprintf("stream=%v/detection=%v", streaming, detectEmpty), func(t *testing.T) {
				calls := 0
				payload := func() []byte {
					calls++
					text := " "
					if calls > 1 {
						text = "continued"
					}
					return fmt.Appendf(nil, `{"id":"resp_%d","object":"response","model":"gpt-6-astra","status":"completed","output":[{"id":"msg_%d","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":%q}]}]}`, calls, calls, text)
				}
				executor := &mockExecutor{
					doFunc: func(_ context.Context, request *httpclient.Request) (*httpclient.Response, error) {
						return &httpclient.Response{StatusCode: 200, Request: request, Body: payload()}, nil
					},
					doStreamFunc: func(context.Context, *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
						body := payload()
						return streams.SliceStream([]*httpclient.StreamEvent{{Type: "response.completed", Data: fmt.Appendf(nil, `{"type":"response.completed","response":%s}`, body)}}), nil
					},
				}
				outbound, err := responses.NewOutboundTransformer("https://example.invalid", "synthetic-key")
				require.NoError(t, err)
				options := []pipeline.Option{pipeline.WithRetry(0, 1, 0)}
				if detectEmpty {
					options = append(options, pipeline.WithEmptyResponseDetection())
				}
				p := pipeline.NewFactory(executor).Pipeline(responses.NewInboundTransformer(), &retryEmptyResponsesOutbound{Outbound: outbound}, options...)
				result, err := p.Process(t.Context(), &httpclient.Request{Method: http.MethodPost, Headers: http.Header{"Content-Type": {"application/json"}}, Body: fmt.Appendf(nil, `{"model":"gpt-6-astra","input":"continue","stream":%v}`, streaming)})
				require.NoError(t, err)
				if streaming {
					defer result.EventStream.Close()
					var frames []byte
					for result.EventStream.Next() {
						frames = append(frames, result.EventStream.Current().Data...)
					}
					require.NoError(t, result.EventStream.Err())
					require.NotContains(t, string(frames), "resp_1")
					require.Contains(t, string(frames), "continued")
				} else {
					require.Equal(t, "continued", gjson.GetBytes(result.Response.Body, "output.0.content.0.text").String())
				}
				require.Equal(t, 2, calls)
			})
		}
	}
}
