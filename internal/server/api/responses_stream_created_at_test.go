package api

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestResponsesStreamFailureCreatedAt(t *testing.T) {
	for _, tc := range []struct {
		name string
		events []string
		want int64
	}{
		{name: "provider timestamp", events: []string{`{"type":"response.created","response":{"id":"resp_1","created_at":1700000000}}`}, want: 1700000000},
		{name: "missing provider timestamp", events: []string{`{"type":"response.created","response":{"id":"resp_1"}}`}},
		{name: "new response resets timestamp", events: []string{`{"type":"response.created","response":{"id":"resp_1","created_at":1700000000}}`, `{"type":"response.created","response":{"id":"resp_2"}}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := time.Now().Unix()
			events := make([]*httpclient.StreamEvent, 0, len(tc.events))
			for _, data := range tc.events {
				events = append(events, &httpclient.StreamEvent{Data: []byte(data)})
			}
			stream, encode := newResponsesStreamAdapter(t.Context(), streams.SliceStream(events), nil)
			defer stream.Close()
			for stream.Next() {
				_ = stream.Current()
			}
			failure, err := encode(context.Background(), io.ErrUnexpectedEOF)
			require.NoError(t, err)
			timestamp := gjson.GetBytes(failure.Data, "response.created_at").Int()
			if tc.want > 0 {
				require.Equal(t, tc.want, timestamp)
			} else {
				require.GreaterOrEqual(t, timestamp, before)
				require.LessOrEqual(t, timestamp, time.Now().Unix())
			}
		})
	}
}
