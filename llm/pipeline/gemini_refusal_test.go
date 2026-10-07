package pipeline_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/gemini"
)

func TestGeminiRefusalPassesEmptyResponseDetection(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, reason := range []string{"SAFETY", "PROHIBITED_CONTENT", "STOP"} {
			t.Run(fmt.Sprintf("%s/stream=%v", reason, stream), func(t *testing.T) {
				payload, err := json.Marshal(map[string]any{"responseId": "blocked", "candidates": []any{map[string]any{"finishReason": reason}}, "usageMetadata": map[string]any{"promptTokenCount": 8}})
				require.NoError(t, err)
				calls := 0
				executor := &mockExecutor{
					doFunc: func(_ context.Context, req *httpclient.Request) (*httpclient.Response, error) {
						calls++
						return &httpclient.Response{StatusCode: http.StatusOK, Body: payload, Request: req}, nil
					},
					doStreamFunc: func(_ context.Context, _ *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
						calls++
						return streams.SliceStream([]*httpclient.StreamEvent{{Data: payload}}), nil
					},
				}
				outbound, err := gemini.NewOutboundTransformer("", "fixture")
				require.NoError(t, err)
				p := pipeline.NewFactory(executor).Pipeline(anthropic.NewInboundTransformer(), outbound, pipeline.WithEmptyResponseDetection())
				result, err := p.Process(t.Context(), &httpclient.Request{Method: http.MethodPost, Headers: http.Header{"Content-Type": []string{"application/json"}}, Body: fmt.Appendf(nil, `{"model":"gemini-3-pro-preview","max_tokens":1000,"stream":%v,"messages":[{"role":"user","content":"hello"}]}`, stream)})
				if reason == "STOP" {
					require.ErrorIs(t, err, pipeline.ErrEmptyResponse, "ordinary empty completions must still be rejected")
					return
				}
				require.NoError(t, err)
				require.Equal(t, 1, calls)
				if stream {
					defer result.EventStream.Close()
					terminal := 0
					for result.EventStream.Next() {
						event := result.EventStream.Current()
						if event.Type == "message_delta" {
							var delta anthropic.StreamEvent
							require.NoError(t, json.Unmarshal(event.Data, &delta))
							require.Equal(t, "refusal", *delta.Delta.StopReason)
							terminal++
						}
					}
					require.NoError(t, result.EventStream.Err())
					require.Equal(t, 1, terminal)
				} else {
					var message anthropic.Message
					require.NoError(t, json.Unmarshal(result.Response.Body, &message))
					require.Equal(t, "refusal", *message.StopReason)
				}
			})
		}
	}
}
