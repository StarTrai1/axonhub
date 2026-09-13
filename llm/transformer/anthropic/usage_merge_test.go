package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestAnthropicStreamUsageUsesPresentCumulativeCounters(t *testing.T) {
	for _, tt := range []struct {
		name       string
		patches    []string
		prompt     int64
		completion int64
		cached     int64
		write      int64
		fiveMinute int64
		oneHour    int64
	}{
		{name: "output only retains input and cache", patches: []string{`{"output_tokens":20}`}, prompt: 115, completion: 20, cached: 100, write: 5, fiveMinute: 5},
		{name: "cache update retains uncached input", patches: []string{`{"output_tokens":20,"cache_read_input_tokens":200}`}, prompt: 215, completion: 20, cached: 200, write: 5, fiveMinute: 5},
		{name: "zero uncached input replaces prior count", patches: []string{`{"output_tokens":20,"input_tokens":0}`}, prompt: 105, completion: 20, cached: 100, write: 5, fiveMinute: 5},
		{name: "explicit zero clears input and cache", patches: []string{`{"output_tokens":20,"input_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"cache_creation":{"ephemeral_5m_input_tokens":0}}`}, completion: 20},
		{name: "null optional counters preserve earlier values", patches: []string{`{"output_tokens":20,"input_tokens":null,"cache_read_input_tokens":null}`}, prompt: 115, completion: 20, cached: 100, write: 5, fiveMinute: 5},
		{name: "partial TTL details retain other window", patches: []string{`{"output_tokens":20,"cache_creation_input_tokens":8,"cache_creation":{"ephemeral_1h_input_tokens":3}}`}, prompt: 118, completion: 20, cached: 100, write: 8, fiveMinute: 5, oneHour: 3},
		{name: "multiple deltas replace rather than add", patches: []string{`{"output_tokens":20,"cache_read_input_tokens":200}`, `{"output_tokens":25,"input_tokens":0}`}, prompt: 205, completion: 25, cached: 200, write: 5, fiveMinute: 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			events := []*httpclient.StreamEvent{{Type: "message_start", Data: []byte(`{"type":"message_start","message":{"id":"msg_usage","type":"message","role":"assistant","model":"claude-test","content":[],"usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":100,"cache_creation_input_tokens":5,"cache_creation":{"ephemeral_5m_input_tokens":5,"ephemeral_1h_input_tokens":0}}}}`)}}
			for _, patch := range tt.patches {
				events = append(events, &httpclient.StreamEvent{Type: "message_delta", Data: []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":` + patch + `}`)})
			}
			events = append(events, &httpclient.StreamEvent{Type: "message_stop", Data: []byte(`{"type":"message_stop"}`)})
			stream := newOutboundStream(streams.SliceStream(events), PlatformDirect)
			defer stream.Close()
			var first, final *llm.Usage
			for stream.Next() {
				if usage := stream.Current().Usage; usage != nil {
					if first == nil {
						first = usage
					}
					final = usage
				}
			}
			require.NoError(t, stream.Err())
			require.NotNil(t, final)
			require.Equal(t, int64(115), first.PromptTokens, "later deltas must not mutate previously emitted usage")
			require.Equal(t, tt.prompt, final.PromptTokens)
			require.Equal(t, tt.completion, final.CompletionTokens)
			require.Equal(t, tt.prompt+tt.completion, final.TotalTokens)
			if tt.cached+tt.write+tt.fiveMinute+tt.oneHour > 0 {
				require.NotNil(t, final.PromptTokensDetails)
				require.Equal(t, tt.cached, final.PromptTokensDetails.CachedTokens)
				require.Equal(t, tt.write, final.PromptTokensDetails.WriteCachedTokens)
				require.Equal(t, tt.fiveMinute, final.PromptTokensDetails.WriteCached5MinTokens)
				require.Equal(t, tt.oneHour, final.PromptTokensDetails.WriteCached1HourTokens)
			} else {
				require.Nil(t, final.PromptTokensDetails)
			}
			body, metadata, err := AggregateStreamChunks(t.Context(), events, PlatformDirect)
			require.NoError(t, err)
			require.Equal(t, final, metadata.Usage)
			var response Message
			require.NoError(t, json.Unmarshal(body, &response))
			require.Equal(t, tt.prompt, response.Usage.InputTokens+response.Usage.CacheReadInputTokens+response.Usage.CacheCreationInputTokens)
		})
	}
}
