package antigravity

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestInBandErrorsPreserveStatusAndSanitizeIdentity(t *testing.T) {
	adapter, err := NewTransformer(Config{APIKey: "synthetic", Project: "fixture"})
	require.NoError(t, err)
	for _, status := range []int{401, 429, 503, 0} {
		for _, wrapped := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/wrapped=%t", status, wrapped), func(t *testing.T) {
				body := fmt.Sprintf(`{"error":{"code":%d,"status":"RESOURCE_EXHAUSTED","message":"Quota for projects/private-project"}}`, status)
				if wrapped {
					body = `{"response":` + body + `}`
				}
				event := &httpclient.StreamEvent{Data: []byte(body)}
				stream, err := adapter.TransformStream(t.Context(), &httpclient.Request{}, streams.SliceStream([]*httpclient.StreamEvent{event}))
				require.NoError(t, err)
				defer stream.Close()
				require.False(t, stream.Next())
				_, _, aggregateErr := adapter.AggregateStreamChunks(t.Context(), nil, []*httpclient.StreamEvent{event})
				_, responseErr := adapter.TransformResponse(t.Context(), &httpclient.Response{StatusCode: 200, Body: []byte(body)})
				for _, failure := range []error{stream.Err(), aggregateErr, responseErr} {
					var converted *llm.ResponseError
					require.True(t, errors.As(failure, &converted))
					want := status
					if want == 0 {
						want = 502
					}
					require.Equal(t, want, converted.StatusCode)
					require.Equal(t, "Quota for projects/***", converted.Detail.Message)
					var raw *httpclient.Error
					require.True(t, errors.As(failure, &raw))
					require.JSONEq(t, body, string(raw.Body))
				}
				require.Equal(t, body, string(event.Data))
			})
		}
	}
}
