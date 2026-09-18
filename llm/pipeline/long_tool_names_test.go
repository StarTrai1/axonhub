package pipeline_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
	"github.com/looplj/axonhub/llm/transformer/openrouter"
)

func TestResponsesLongToolNamesPipeline(t *testing.T) {
	namespace := "mcp__" + strings.Repeat("workspace_", 8)
	for _, factory := range []func(string, string) (transformer.Outbound, error){openai.NewOutboundTransformer, openrouter.NewOutboundTransformer} {
		for _, stream := range []bool{false, true} {
			outbound, err := factory("https://example.com", "test")
			require.NoError(t, err)
			name := func(req *httpclient.Request) string {
				var body openai.Request
				require.NoError(t, json.Unmarshal(req.Body, &body))
				require.Len(t, body.Tools, 1)
				alias := body.Tools[0].Function.Name
				require.LessOrEqual(t, len(alias), 64)
				require.NotEqual(t, namespace+"__search", alias)
				return alias
			}
			executor := namespaceExecutor{
				do: func(req *httpclient.Request) (*httpclient.Response, error) {
					return &httpclient.Response{StatusCode: 200, Request: req, Body: []byte(fmt.Sprintf(`{"id":"resp_long","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":%q,"arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`, name(req)))}, nil
				},
				stream: func(req *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
					return streams.NoNil(streams.SliceStream([]*httpclient.StreamEvent{
						{Data: []byte(fmt.Sprintf(`{"id":"resp_long","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":%q,"arguments":""}}]}}]}`, name(req)))},
						{Data: []byte(`{"id":"resp_long","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]}}]}`)},
						{Data: []byte(`{"id":"resp_long","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`)},
						{Data: []byte(`[DONE]`)},
					})), nil
				},
			}
			pipe := pipeline.NewFactory(executor).Pipeline(responses.NewInboundTransformer(), outbound)
			result, err := pipe.Process(t.Context(), &httpclient.Request{Body: []byte(fmt.Sprintf(`{"model":"test","stream":%t,"input":"hello","tools":[{"type":"namespace","name":%q,"tools":[{"type":"function","name":"search"}]}]}`, stream, namespace))})
			require.NoError(t, err)
			var response responses.Response
			if stream {
				for result.EventStream.Next() {
					var event responses.StreamEvent
					require.NoError(t, json.Unmarshal(result.EventStream.Current().Data, &event))
					if event.Type == "response.completed" {
						require.NotNil(t, event.Response)
						response = *event.Response
					}
				}
				require.NoError(t, result.EventStream.Err())
				require.NoError(t, result.EventStream.Close())
			} else {
				require.NoError(t, json.Unmarshal(result.Response.Body, &response))
			}
			require.Len(t, response.Output, 1)
			require.Equal(t, "search", response.Output[0].Name)
			require.Equal(t, namespace, response.Output[0].Namespace)
			require.Equal(t, "call_1", response.Output[0].CallID)
		}
	}
}
