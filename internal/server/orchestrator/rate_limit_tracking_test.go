package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestCodexUsageLimitResetCooldown(t *testing.T) {
	now := time.Unix(1800000000, 0)
	for _, testCase := range []struct {
		name string
		body string
		want time.Duration
	}{
		{"nested seconds", `{"error":{"type":"usage_limit_reached","resets_in_seconds":18000}}`, 5 * time.Hour},
		{"flat string seconds", `{"type":"USAGE_LIMIT_REACHED","resets_in_seconds":"604800"}`, 7 * 24 * time.Hour},
		{"timestamp wins", `{"error":{"type":"usage_limit_reached","resets_at":"1800018000","resets_in_seconds":10}}`, 5 * time.Hour},
		{"past timestamp falls back", `{"type":"usage_limit_reached","resets_at":1799999999,"resets_in_seconds":30}`, 30 * time.Second},
		{"wrong error", `{"error":{"type":"rate_limit_exceeded","resets_in_seconds":18000}}`, 0},
		{"missing type", `{"resets_in_seconds":18000}`, 0},
		{"negative", `{"type":"usage_limit_reached","resets_in_seconds":-1}`, 0},
		{"overflow", `{"type":"usage_limit_reached","resets_in_seconds":9223372036854775807}`, 0},
		{"fraction", `{"type":"usage_limit_reached","resets_in_seconds":1.5}`, 0},
		{"malformed", `{"error":`, 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := fmt.Errorf("wrapped: %w", &httpclient.Error{StatusCode: http.StatusTooManyRequests, Body: []byte(testCase.body)})
			cooldown, ok := codexUsageLimitResetCooldown(err, now)
			require.Equal(t, testCase.want > 0, ok)
			require.Equal(t, testCase.want, cooldown)
		})
	}
	_, ok := codexUsageLimitResetCooldown(&httpclient.Error{StatusCode: 400, Body: []byte(`{"type":"usage_limit_reached","resets_in_seconds":10}`)}, now)
	require.False(t, ok)
}

func TestCodexUsageLimitCooldownDoesNotDisableOtherCredentials(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		channelType entchannel.Type
		keys        []string
		want        bool
	}{
		{"single Codex credential", entchannel.TypeCodex, []string{"test-key-one"}, true},
		{"multiple Codex credentials", entchannel.TypeCodex, []string{"test-key-one", "test-key-two"}, false},
		{"other provider", entchannel.TypeOpenai, []string{"test-key-one"}, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tracker := NewChannelRequestTracker()
			current := &biz.Channel{Channel: &ent.Channel{
				ID: 1, Type: testCase.channelType,
				Credentials: objects.ChannelCredentials{APIKeys: testCase.keys},
			}}
			middleware := &rateLimitTracking{
				outbound: &PersistentOutboundTransformer{state: &PersistenceState{
					CurrentCandidate: &ChannelModelsCandidate{Channel: current},
				}},
				tracker: tracker,
			}
			before := time.Now()
			middleware.OnOutboundRawError(context.Background(), &httpclient.Error{
				StatusCode: http.StatusTooManyRequests,
				Body:       []byte(`{"error":{"type":"usage_limit_reached","resets_in_seconds":18000}}`),
			})
			until, ok := tracker.GetCooldownUntil(1)
			require.Equal(t, testCase.want, ok)
			if ok {
				require.WithinDuration(t, before.Add(5*time.Hour), until, time.Second)
			}
		})
	}
}

func TestRateLimitTracking_OnOutboundLlmResponse(t *testing.T) {
	tracker := NewChannelRequestTracker()

	entChannel := &ent.Channel{
		ID:   1,
		Name: "test-channel",
	}
	channel := &biz.Channel{
		Channel: entChannel,
	}

	state := &PersistenceState{
		CurrentCandidate: &ChannelModelsCandidate{
			Channel: channel,
		},
	}
	outbound := &PersistentOutboundTransformer{
		state: state,
	}

	middleware := &rateLimitTracking{
		outbound: outbound,
		tracker:  tracker,
	}

	tests := []struct {
		name           string
		response       *llm.Response
		expectedTokens int64
	}{
		{
			name: "tracks tokens from response",
			response: &llm.Response{
				Usage: &llm.Usage{
					TotalTokens: 150,
				},
			},
			expectedTokens: 150,
		},
		{
			name: "handles nil usage",
			response: &llm.Response{
				Usage: nil,
			},
			expectedTokens: 0,
		},
		{
			name: "handles zero tokens",
			response: &llm.Response{
				Usage: &llm.Usage{
					TotalTokens: 0,
				},
			},
			expectedTokens: 0,
		},
		{
			name:           "handles nil response",
			response:       nil,
			expectedTokens: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset tracker
			tracker = NewChannelRequestTracker()
			middleware.tracker = tracker

			ctx := context.Background()
			result, err := middleware.OnOutboundLlmResponse(ctx, tt.response)

			assert.NoError(t, err)
			assert.Equal(t, tt.response, result)
			assert.Equal(t, tt.expectedTokens, tracker.GetTokenCount(channel.ID))
		})
	}
}

func TestRateLimitTracking_OnOutboundLlmResponse_MultipleChannels(t *testing.T) {
	tracker := NewChannelRequestTracker()

	entChannel1 := &ent.Channel{ID: 1, Name: "channel-1"}
	entChannel2 := &ent.Channel{ID: 2, Name: "channel-2"}

	channel1 := &biz.Channel{Channel: entChannel1}
	channel2 := &biz.Channel{Channel: entChannel2}

	state1 := &PersistenceState{
		CurrentCandidate: &ChannelModelsCandidate{Channel: channel1},
	}
	state2 := &PersistenceState{
		CurrentCandidate: &ChannelModelsCandidate{Channel: channel2},
	}

	outbound1 := &PersistentOutboundTransformer{state: state1}
	outbound2 := &PersistentOutboundTransformer{state: state2}

	middleware1 := &rateLimitTracking{outbound: outbound1, tracker: tracker}
	middleware2 := &rateLimitTracking{outbound: outbound2, tracker: tracker}

	ctx := context.Background()

	// Add tokens for channel 1
	_, _ = middleware1.OnOutboundLlmResponse(ctx, &llm.Response{
		Usage: &llm.Usage{TotalTokens: 100},
	})

	// Add tokens for channel 2
	_, _ = middleware2.OnOutboundLlmResponse(ctx, &llm.Response{
		Usage: &llm.Usage{TotalTokens: 200},
	})

	assert.Equal(t, int64(100), tracker.GetTokenCount(1))
	assert.Equal(t, int64(200), tracker.GetTokenCount(2))
}

func TestRateLimitTracking_OnOutboundLlmStream(t *testing.T) {
	tracker := NewChannelRequestTracker()

	entChannel := &ent.Channel{ID: 1, Name: "test-channel"}
	channel := &biz.Channel{Channel: entChannel}

	state := &PersistenceState{
		CurrentCandidate: &ChannelModelsCandidate{Channel: channel},
	}
	outbound := &PersistentOutboundTransformer{state: state}

	middleware := &rateLimitTracking{
		outbound: outbound,
		tracker:  tracker,
	}

	// Create mock stream with usage in last chunk
	events := []*llm.Response{
		{ID: "1", Usage: nil},
		{ID: "2", Usage: nil},
		{ID: "3", Usage: &llm.Usage{TotalTokens: 250}},
	}

	mockStream := streams.SliceStream(events)

	ctx := context.Background()
	wrappedStream, err := middleware.OnOutboundLlmStream(ctx, mockStream)
	assert.NoError(t, err)

	// Consume the stream
	tokenCount := int64(0)

	for wrappedStream.Next() {
		event := wrappedStream.Current()
		if event != nil && event.Usage != nil {
			tokenCount = event.Usage.TotalTokens
		}
	}

	assert.NoError(t, wrappedStream.Err())
	assert.Equal(t, int64(250), tokenCount)
	assert.Equal(t, int64(250), tracker.GetTokenCount(channel.ID))
}

func TestRateLimitTracking_OnOutboundRawRequest_DoesNotIncrementRequests(t *testing.T) {
	tracker := NewChannelRequestTracker()

	entChannel := &ent.Channel{ID: 1, Name: "test-channel"}
	channel := &biz.Channel{Channel: entChannel}

	state := &PersistenceState{
		CurrentCandidate: &ChannelModelsCandidate{Channel: channel},
	}
	outbound := &PersistentOutboundTransformer{state: state}

	middleware := &rateLimitTracking{
		outbound: outbound,
		tracker:  tracker,
	}

	ctx := context.Background()

	for range 5 {
		_, err := middleware.OnOutboundRawRequest(ctx, nil)
		assert.NoError(t, err)
	}

	assert.Equal(t, int64(0), tracker.GetRequestCount(channel.ID))
}

func TestRateLimitTracking_CombinedTokenOnly(t *testing.T) {
	tracker := NewChannelRequestTracker()

	entChannel := &ent.Channel{ID: 1, Name: "test-channel"}
	channel := &biz.Channel{Channel: entChannel}

	state := &PersistenceState{
		CurrentCandidate: &ChannelModelsCandidate{Channel: channel},
	}
	outbound := &PersistentOutboundTransformer{state: state}

	middleware := &rateLimitTracking{
		outbound: outbound,
		tracker:  tracker,
	}

	ctx := context.Background()

	// Simulate a request flow. RPM admission is handled before this middleware;
	// tracking only records TPM and upstream cooldown signals.
	_, _ = middleware.OnOutboundRawRequest(ctx, nil)
	_, _ = middleware.OnOutboundRawRequest(ctx, nil)

	_, _ = middleware.OnOutboundLlmResponse(ctx, &llm.Response{
		Usage: &llm.Usage{TotalTokens: 100},
	})

	_, _ = middleware.OnOutboundRawRequest(ctx, nil)

	_, _ = middleware.OnOutboundLlmResponse(ctx, &llm.Response{
		Usage: &llm.Usage{TotalTokens: 50},
	})

	assert.Equal(t, int64(0), tracker.GetRequestCount(channel.ID))
	assert.Equal(t, int64(150), tracker.GetTokenCount(channel.ID))
}

func TestNoopRateLimitTracking(t *testing.T) {
	middleware := &noopRateLimitTracking{}

	ctx := context.Background()

	// Should return response unchanged
	resp := &llm.Response{ID: "test"}
	result, err := middleware.OnOutboundLlmResponse(ctx, resp)
	assert.NoError(t, err)
	assert.Equal(t, resp, result)

	// Should return stream unchanged
	stream := streams.SliceStream([]*llm.Response{{ID: "1"}})
	wrappedStream, err := middleware.OnOutboundLlmStream(ctx, stream)
	assert.NoError(t, err)
	assert.Equal(t, stream, wrappedStream)
}

// ========== OnOutboundRawError Tests (429 Cooldown) ==========

func TestRateLimitTracking_OnOutboundRawError_429(t *testing.T) {
	tracker := NewChannelRequestTracker()

	entChannel := &ent.Channel{ID: 1, Name: "test-channel"}
	channel := &biz.Channel{Channel: entChannel}

	state := &PersistenceState{
		CurrentCandidate: &ChannelModelsCandidate{Channel: channel},
	}
	outbound := &PersistentOutboundTransformer{state: state}

	middleware := &rateLimitTracking{
		outbound: outbound,
		tracker:  tracker,
	}

	ctx := context.Background()

	// Simulate 429 error with Retry-After header
	httpErr := &httpclient.Error{
		StatusCode: http.StatusTooManyRequests,
		Headers:    http.Header{"Retry-After": []string{"30"}},
	}

	middleware.OnOutboundRawError(ctx, httpErr)

	// Verify channel is in cooldown
	assert.True(t, tracker.IsCoolingDown(channel.ID))
}

//nolint:exhaustruct // Test initializes only fields used by rate-limit tracking.
func TestRateLimitTracking_OnOutboundRawError_CodexExhaustedWindowUsesReset(t *testing.T) {
	tracker := NewChannelRequestTracker()
	channel := &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "codex"}}
	middleware := &rateLimitTracking{
		outbound: &PersistentOutboundTransformer{state: &PersistenceState{
			CurrentCandidate: &ChannelModelsCandidate{Channel: channel},
		}},
		tracker: tracker,
	}
	now := time.Now()

	middleware.OnOutboundRawError(context.Background(), &httpclient.Error{
		StatusCode: http.StatusTooManyRequests,
		Headers: http.Header{
			"X-Codex-Primary-Used-Percent":        []string{"100"},
			"X-Codex-Primary-Reset-After-Seconds": []string{"18000"},
			"Retry-After":                         []string{"30"},
		},
	})

	until, ok := tracker.GetCooldownUntil(channel.ID)
	require.True(t, ok)
	require.WithinDuration(t, now.Add(5*time.Hour), until, time.Second)
}

//nolint:exhaustruct // Test initializes only fields used by the parser.
func TestCodexQuotaResetCooldownUsesLongestExhaustedWindow(t *testing.T) {
	cooldown, ok := codexQuotaResetCooldown(&httpclient.Error{
		StatusCode: http.StatusTooManyRequests,
		Headers: http.Header{
			"X-Codex-Primary-Used-Percent":          []string{"100"},
			"X-Codex-Primary-Reset-After-Seconds":   []string{"18000"},
			"X-Codex-Secondary-Used-Percent":        []string{"100.0"},
			"X-Codex-Secondary-Reset-After-Seconds": []string{"604800"},
		},
	})

	require.True(t, ok)
	require.Equal(t, 7*24*time.Hour, cooldown)
}

func TestRateLimitTracking_OnOutboundRawError_QueueErrorIgnored(t *testing.T) {
	tracker := NewChannelRequestTracker()

	channel := &biz.Channel{Channel: &ent.Channel{ID: 7, Name: "kimi"}}

	state := &PersistenceState{
		CurrentCandidate: &ChannelModelsCandidate{Channel: channel},
	}
	outbound := &PersistentOutboundTransformer{state: state}

	middleware := &rateLimitTracking{
		outbound: outbound,
		tracker:  tracker,
	}

	queueErr := asChannelQueueError(channel, ErrChannelQueueFull)
	require.NotNil(t, queueErr)

	middleware.OnOutboundRawError(context.Background(), queueErr)

	assert.False(t, tracker.IsCoolingDown(channel.ID),
		"local queue rejection must not trigger upstream-style cooldown")
}

func TestRateLimitTracking_OnOutboundRawError_429WithoutRetryAfter(t *testing.T) {
	tracker := NewChannelRequestTracker()

	entChannel := &ent.Channel{ID: 1, Name: "test-channel"}
	channel := &biz.Channel{Channel: entChannel}

	state := &PersistenceState{
		CurrentCandidate: &ChannelModelsCandidate{Channel: channel},
	}
	outbound := &PersistentOutboundTransformer{state: state}

	middleware := &rateLimitTracking{
		outbound: outbound,
		tracker:  tracker,
	}

	ctx := context.Background()

	httpErr := &httpclient.Error{
		StatusCode: http.StatusTooManyRequests,
		Headers:    http.Header{},
	}

	middleware.OnOutboundRawError(ctx, httpErr)

	assert.False(t, tracker.IsCoolingDown(channel.ID))
}

func TestRateLimitTracking_OnOutboundRawError_Not429(t *testing.T) {
	tracker := NewChannelRequestTracker()

	entChannel := &ent.Channel{ID: 1, Name: "test-channel"}
	channel := &biz.Channel{Channel: entChannel}

	state := &PersistenceState{
		CurrentCandidate: &ChannelModelsCandidate{Channel: channel},
	}
	outbound := &PersistentOutboundTransformer{state: state}

	middleware := &rateLimitTracking{
		outbound: outbound,
		tracker:  tracker,
	}

	ctx := context.Background()

	// Simulate 500 error (not 429)
	httpErr := &httpclient.Error{
		StatusCode: http.StatusInternalServerError,
		Headers:    http.Header{"Retry-After": []string{"30"}},
	}

	middleware.OnOutboundRawError(ctx, httpErr)

	// Verify channel is NOT in cooldown
	assert.False(t, tracker.IsCoolingDown(channel.ID))
}

func TestRateLimitTracking_OnOutboundRawError_NoChannel(t *testing.T) {
	tracker := NewChannelRequestTracker()

	state := &PersistenceState{
		CurrentCandidate: nil, // No current channel
	}
	outbound := &PersistentOutboundTransformer{state: state}

	middleware := &rateLimitTracking{
		outbound: outbound,
		tracker:  tracker,
	}

	ctx := context.Background()

	// Simulate 429 error
	httpErr := &httpclient.Error{
		StatusCode: http.StatusTooManyRequests,
		Headers:    http.Header{"Retry-After": []string{"30"}},
	}

	// Should not panic
	middleware.OnOutboundRawError(ctx, httpErr)
}

func TestRateLimitTracking_OnOutboundRawError_NilChannel(t *testing.T) {
	tracker := NewChannelRequestTracker()

	outbound := &PersistentOutboundTransformer{}

	middleware := &rateLimitTracking{
		outbound: outbound,
		tracker:  tracker,
	}

	ctx := context.Background()

	// Simulate 429 error
	httpErr := &httpclient.Error{
		StatusCode: http.StatusTooManyRequests,
		Headers:    http.Header{"Retry-After": []string{"30"}},
	}

	// Should not panic
	middleware.OnOutboundRawError(ctx, httpErr)
}

// ========== parseRetryAfter tests moved to llm/httpclient/errors_test.go ==========
