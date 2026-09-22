package httpclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const (
	// DefaultRetryAfterSeconds is the default cooldown duration when Retry-After header is missing or invalid.
	DefaultRetryAfterSeconds = 60

	// MaxRetryAfterDuration is the maximum cooldown duration to prevent channels from being unavailable for too long.
	MaxRetryAfterDuration = 5 * time.Minute
)

func IsNotFoundErr(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.StatusCode == http.StatusNotFound
}

func IsRateLimitErr(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.StatusCode == http.StatusTooManyRequests
}

// HasRetryAfterHeader returns true if the error is a 429 rate limit error with a Retry-After header.
func HasRetryAfterHeader(err error) bool {
	var httpErr *Error
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTooManyRequests {
		return false
	}

	return httpErr.Headers != nil && httpErr.Headers.Get("Retry-After") != ""
}

// ParseRetryAfter parses the Retry-After header from a 429 error according to RFC 7231.
// Returns (duration, true) if the error is a 429 rate limit error, (0, false) otherwise.
// If Retry-After header is missing or invalid, returns DefaultRetryAfterSeconds.
// The duration is capped at MaxRetryAfterDuration.
func ParseRetryAfter(err error) (time.Duration, bool) {
	var httpErr *Error
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}

	value := httpErr.Headers.Get("Retry-After")
	if value == "" {
		return DefaultRetryAfterSeconds * time.Second, true
	}

	// 1. Try to parse as integer seconds (delta-seconds)
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds <= 0 {
			return 0, true
		}

		duration := time.Duration(seconds) * time.Second

		return min(duration, MaxRetryAfterDuration), true
	}

	// 2. Try to parse as HTTP-date
	if t, err := http.ParseTime(value); err == nil {
		duration := time.Until(t)
		if duration <= 0 {
			return 0, true
		}

		return min(duration, MaxRetryAfterDuration), true
	}

	// 3. Failed to parse, use default
	return DefaultRetryAfterSeconds * time.Second, true
}

// ParseGoogleRetryInfo reads an explicit Google RPC retry delay on a 429.
// Unlike ParseRetryAfter, missing or invalid advice does not invent a cooldown.
func ParseGoogleRetryInfo(err error) (time.Duration, bool) {
	var httpErr *Error
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	var envelope struct {
		Error struct {
			Details []json.RawMessage `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(httpErr.Body, &envelope) != nil {
		return 0, false
	}
	var longest time.Duration
	for _, raw := range envelope.Error.Details {
		var detail struct {
			Type       string `json:"@type"`
			RetryDelay string `json:"retryDelay"`
		}
		if json.Unmarshal(raw, &detail) != nil || detail.Type != "type.googleapis.com/google.rpc.RetryInfo" {
			continue
		}
		if delay, parseErr := time.ParseDuration(detail.RetryDelay); parseErr == nil && delay > 0 {
			longest = max(longest, min(delay, MaxRetryAfterDuration))
		}
	}
	return longest, longest > 0
}

type Error struct {
	Method     string      `json:"method"`
	URL        string      `json:"url"`
	StatusCode int         `json:"status_code"`
	Status     string      `json:"status"`
	Body       []byte      `json:"body"`
	Headers    http.Header `json:"-"` // HTTP response headers (not serialized)
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s - %s with status %s", e.Method, e.URL, e.Status)
}
