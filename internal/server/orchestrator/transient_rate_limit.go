package orchestrator

import (
	"errors"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/looplj/axonhub/llm/httpclient"
)

const maxTransientRateLimitWait = time.Minute

func canRetryTransientRateLimit(err error) bool {
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"insufficient_quota", "usage_limit_reached", "billing_hard_limit", "quota exhausted", "insufficient quota"} {
		if strings.Contains(message, marker) {
			return false
		}
	}
	var raw *httpclient.Error
	if errors.As(err, &raw) {
		body := strings.ToLower(string(raw.Body))
		for _, marker := range []string{"insufficient_quota", "usage_limit_reached", "billing_hard_limit", "quota exhausted", "insufficient quota"} {
			if strings.Contains(body, marker) {
				return false
			}
		}
	}
	if cooldown, ok := codexQuotaResetCooldown(err); ok && cooldown > maxTransientRateLimitWait {
		return false
	}
	delay, _ := transientRateLimitRetryAfter(err, time.Now())
	return delay <= maxTransientRateLimitWait
}

func transientRateLimitRetryAfter(err error, now time.Time) (time.Duration, bool) {
	var raw *httpclient.Error
	if !errors.As(err, &raw) || raw.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	for _, header := range []string{"Retry-After", "Retry-After-Ms", "X-Ms-Retry-After-Ms"} {
		value := strings.TrimSpace(raw.Headers.Get(header))
		if value == "" {
			continue
		}
		if header == "Retry-After" {
			if reset, parseErr := http.ParseTime(value); parseErr == nil {
				return max(0, reset.Sub(now)), true
			}
		}
		amount, parseErr := strconv.ParseFloat(value, 64)
		if parseErr != nil || math.IsNaN(amount) || amount < 0 {
			continue
		}
		unit := time.Second
		if header != "Retry-After" {
			unit = time.Millisecond
		}
		if amount > float64(maxTransientRateLimitWait)/float64(unit) {
			return maxTransientRateLimitWait + time.Second, true
		}
		return time.Duration(amount * float64(unit)), true
	}
	return 0, false
}

func (p *PersistentOutboundTransformer) SameChannelRetryDelay(err error, attempt int) time.Duration {
	if ExtractStatusCodeFromError(err) != http.StatusTooManyRequests {
		return 0
	}
	delay := time.Second * time.Duration(1<<min(max(attempt, 1), 5))
	if explicit, ok := transientRateLimitRetryAfter(err, time.Now()); ok {
		delay = max(delay, explicit)
	}
	return delay + time.Duration(rand.IntN(501))*time.Millisecond
}
