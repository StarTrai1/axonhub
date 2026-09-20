package orchestrator

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestResponsesQuotaResetSurvivesProtocolConversion(t *testing.T) {
	outbound, err := responses.NewOutboundTransformer("https://example.invalid", "synthetic-key")
	require.NoError(t, err)
	now := time.Unix(1800000000, 0)
	for _, tc := range []struct {
		name   string
		fields string
		want   time.Duration
	}{
		{name: "seconds", fields: `"resets_in_seconds":7200`, want: 2 * time.Hour},
		{name: "timestamp wins", fields: `"resets_at":"1800018000","resets_in_seconds":7200`, want: 5 * time.Hour},
		{name: "negative", fields: `"resets_in_seconds":-1`},
		{name: "overflow", fields: `"resets_in_seconds":9223372036854775807`},
		{name: "invalid shape", fields: `"resets_in_seconds":{"unexpected":1}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			detail := fmt.Sprintf(`{"type":"usage_limit_reached","message":"quota exhausted",%s}`, tc.fields)
			response := fmt.Sprintf(`{"id":"resp_failed","status":"failed","error":%s}`, detail)
			event := []byte(fmt.Sprintf(`{"type":"error","status":429,"error":%s}`, detail))
			for _, path := range []string{"websocket collected", "error stream", "failed stream", "failed response"} {
				t.Run(path, func(t *testing.T) {
					var failure error
					switch path {
					case "websocket collected":
						failure = responses.TopLevelWebSocketError([]*httpclient.StreamEvent{{Type: "error", Data: event}})
					case "failed response":
						_, failure = outbound.TransformResponse(t.Context(), &httpclient.Response{StatusCode: 200, Body: []byte(response)})
					default:
						chunk := &httpclient.StreamEvent{Type: "error", Data: event}
						if path == "failed stream" {
							chunk = &httpclient.StreamEvent{Type: "response.failed", Data: []byte(fmt.Sprintf(`{"type":"response.failed","response":%s}`, response))}
						}
						stream, streamErr := outbound.TransformStream(t.Context(), nil, streams.SliceStream([]*httpclient.StreamEvent{chunk}))
						require.NoError(t, streamErr)
						_, failure = streams.All(stream)
					}
					require.Error(t, failure)
					var raw *httpclient.Error
					require.ErrorAs(t, failure, &raw)
					require.Equal(t, 429, raw.StatusCode)
					cooldown, ok := codexUsageLimitResetCooldown(failure, now)
					require.Equal(t, tc.want > 0, ok)
					require.Equal(t, tc.want, cooldown)
				})
			}
		})
	}
}
