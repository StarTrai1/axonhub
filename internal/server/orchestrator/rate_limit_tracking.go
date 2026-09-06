package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
)

// withRateLimitTracking creates a middleware that tracks token usage and
// provider cooldown signals for rate limiting.
func withRateLimitTracking(outbound *PersistentOutboundTransformer, tracker *ChannelRequestTracker) pipeline.Middleware {
	if tracker == nil {
		return &noopRateLimitTracking{}
	}

	return &rateLimitTracking{
		outbound: outbound,
		tracker:  tracker,
	}
}

// rateLimitTracking records TPM usage and provider cooldown signals. RPM
// accounting is owned by rateLimitAdmissionMiddleware so request slots are
// consumed atomically before an attempt reaches upstream.
type rateLimitTracking struct {
	pipeline.DummyMiddleware

	outbound *PersistentOutboundTransformer
	tracker  *ChannelRequestTracker
}

func (m *rateLimitTracking) Name() string {
	return "track-rate-limit"
}

func (m *rateLimitTracking) OnOutboundLlmResponse(ctx context.Context, response *llm.Response) (*llm.Response, error) {
	channel := m.outbound.GetCurrentChannel()
	if channel == nil || response == nil || response.Usage == nil {
		return response, nil
	}

	totalTokens := response.Usage.TotalTokens
	if totalTokens > 0 {
		m.tracker.AddTokens(channel.ID, totalTokens)

		if log.DebugEnabled(ctx) {
			log.Debug(ctx, "Incremented rate limit token count",
				log.Int("channel_id", channel.ID),
				log.String("channel_name", channel.Name),
				log.Int64("tokens", totalTokens),
				log.Int64("current_tpm", m.tracker.GetTokenCount(channel.ID)),
			)
		}
	}

	return response, nil
}

func (m *rateLimitTracking) OnOutboundLlmStream(ctx context.Context, stream streams.Stream[*llm.Response]) (streams.Stream[*llm.Response], error) {
	return &rateLimitTrackingStream{
		ctx:      ctx,
		stream:   stream,
		tracker:  m.tracker,
		outbound: m.outbound,
	}, nil
}

// OnOutboundRawError handles raw HTTP errors, specifically capturing 429 Too Many Requests.
// When a 429 is received, it parses the Retry-After header and sets a cooldown for the channel.
func (m *rateLimitTracking) OnOutboundRawError(ctx context.Context, err error) {
	if m.outbound == nil {
		return
	}

	// Local admission rejections never reached upstream — they must not trigger cooldown.
	if isChannelQueueError(err) || isLocalRPMExhaustedError(err) {
		return
	}

	currentChannel := m.outbound.GetCurrentChannel()
	if currentChannel == nil {
		return
	}
	if currentChannel.Type == channel.TypeCodex &&
		len(currentChannel.Credentials.GetAllCredentialRefs()) == 1 {
		if cooldown, ok := codexUsageLimitResetCooldown(err, time.Now()); ok {
			if headerCooldown, explicit := codexQuotaResetCooldown(err); explicit {
				cooldown = max(cooldown, headerCooldown)
			}
			m.tracker.SetCooldown(currentChannel.ID, time.Now().Add(cooldown))
			log.Warn(ctx, "Codex credential cooling down until usage limit resets",
				log.Int("channel_id", currentChannel.ID),
				log.Duration("cooldown", cooldown),
			)
			return
		}
	}

	// Codex exposes the exhausted quota window and its reset delay separately
	// from Retry-After. Honor that explicit signal without the generic five
	// minute cap so an exhausted 5-hour/weekly channel is not retried repeatedly.
	if cooldown, ok := codexQuotaResetCooldown(err); ok {
		m.tracker.SetCooldown(currentChannel.ID, time.Now().Add(cooldown))
		log.Warn(ctx, "channel cooling down until Codex quota reset",
			log.Int("channel_id", currentChannel.ID),
			log.String("channel_name", currentChannel.Name),
			log.Duration("cooldown", cooldown),
		)

		return
	}

	// Only cool down a channel when the upstream explicitly provides a cooldown.
	if !httpclient.HasRetryAfterHeader(err) {
		return
	}

	// Parse Retry-After header from 429 error
	cooldown, ok := httpclient.ParseRetryAfter(err)
	if !ok {
		return
	}

	// Set cooldown for this channel
	m.tracker.SetCooldown(currentChannel.ID, time.Now().Add(cooldown))

	log.Warn(ctx, "channel cooling down due to 429",
		log.Int("channel_id", currentChannel.ID),
		log.String("channel_name", currentChannel.Name),
		log.Duration("cooldown", cooldown),
	)
}

func codexUsageLimitResetCooldown(err error, now time.Time) (time.Duration, bool) {
	var httpErr *httpclient.Error
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	type quotaError struct {
		Type            string          `json:"type"`
		ResetsAt        json.RawMessage `json:"resets_at"`
		ResetsInSeconds json.RawMessage `json:"resets_in_seconds"`
	}
	var envelope struct {
		quotaError
		Error *quotaError `json:"error"`
	}
	if json.Unmarshal(httpErr.Body, &envelope) != nil {
		return 0, false
	}
	for _, quota := range []*quotaError{envelope.Error, &envelope.quotaError} {
		if quota == nil || !strings.EqualFold(strings.TrimSpace(quota.Type), "usage_limit_reached") {
			continue
		}
		var resetAt, resetIn int64
		if value, parseErr := strconv.ParseInt(strings.Trim(string(quota.ResetsAt), `"`), 10, 64); parseErr == nil {
			resetAt = value
		}
		if value, parseErr := strconv.ParseInt(strings.Trim(string(quota.ResetsInSeconds), `"`), 10, 64); parseErr == nil {
			resetIn = value
		}
		if resetAt > now.Unix() && resetAt-now.Unix() <= math.MaxInt64/int64(time.Second) {
			return time.Unix(resetAt, 0).Sub(now), true
		}
		if resetIn > 0 && resetIn <= math.MaxInt64/int64(time.Second) {
			return time.Duration(resetIn) * time.Second, true
		}
	}
	return 0, false
}

func codexQuotaResetCooldown(err error) (time.Duration, bool) {
	var httpErr *httpclient.Error
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTooManyRequests || httpErr.Headers == nil {
		return 0, false
	}

	var longest time.Duration
	for _, window := range []string{"primary", "secondary"} {
		prefix := "x-codex-" + window + "-"
		used, parseUsedErr := strconv.ParseFloat(strings.TrimSpace(httpErr.Headers.Get(prefix+"used-percent")), 64)
		resetSeconds, parseResetErr := strconv.ParseInt(strings.TrimSpace(httpErr.Headers.Get(prefix+"reset-after-seconds")), 10, 64)
		if parseUsedErr != nil || parseResetErr != nil || math.IsNaN(used) || math.IsInf(used, 0) || used < 100 || resetSeconds <= 0 || resetSeconds > math.MaxInt64/int64(time.Second) {
			continue
		}

		longest = max(longest, time.Duration(resetSeconds)*time.Second)
	}

	return longest, longest > 0
}

// rateLimitTrackingStream wraps a stream to track token usage for rate limiting.
//
//nolint:containedctx // ctx is used for logging.
type rateLimitTrackingStream struct {
	ctx      context.Context
	stream   streams.Stream[*llm.Response]
	tracker  *ChannelRequestTracker
	outbound *PersistentOutboundTransformer
}

func (s *rateLimitTrackingStream) Current() *llm.Response {
	event := s.stream.Current()
	if event == nil {
		return event
	}

	// Track tokens if usage information is present (typically in the last chunk)
	if event.Usage != nil && event.Usage.TotalTokens > 0 {
		channel := s.outbound.GetCurrentChannel()
		if channel != nil {
			s.tracker.AddTokens(channel.ID, event.Usage.TotalTokens)

			if log.DebugEnabled(s.ctx) {
				log.Debug(s.ctx, "Incremented rate limit token count from stream",
					log.Int("channel_id", channel.ID),
					log.String("channel_name", channel.Name),
					log.Int64("tokens", event.Usage.TotalTokens),
					log.Int64("current_tpm", s.tracker.GetTokenCount(channel.ID)),
				)
			}
		}
	}

	return event
}

func (s *rateLimitTrackingStream) Next() bool {
	return s.stream.Next()
}

func (s *rateLimitTrackingStream) Close() error {
	return s.stream.Close()
}

func (s *rateLimitTrackingStream) Err() error {
	return s.stream.Err()
}

// noopRateLimitTracking is a no-op middleware when rate limit tracking is disabled.
type noopRateLimitTracking struct {
	pipeline.DummyMiddleware
}

func (m *noopRateLimitTracking) Name() string {
	return "track-rate-limit-noop"
}

func (m *noopRateLimitTracking) OnOutboundLlmStream(ctx context.Context, stream streams.Stream[*llm.Response]) (streams.Stream[*llm.Response], error) {
	return stream, nil
}
