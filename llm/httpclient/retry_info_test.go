package httpclient

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseGoogleRetryInfo(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		details string
		want    time.Duration
	}{
		{"seconds", `[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"39s"}]`, 39 * time.Second},
		{"fractional", `[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"1.5s"}]`, 1500 * time.Millisecond},
		{"bounded", `[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"86400s"}]`, MaxRetryAfterDuration},
		{"longest advice", `[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"2s"},{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"3s"}]`, 3 * time.Second},
		{"wrong type", `[{"@type":"unrelated","retryDelay":"39s"}]`, 0},
		{"missing type", `[{"retryDelay":"39s"}]`, 0},
		{"negative", `[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"-1s"}]`, 0},
		{"zero", `[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"0s"}]`, 0},
		{"invalid", `[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"later"}]`, 0},
		{"overflow", `[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"999999999999999999999s"}]`, 0},
		{"skip malformed detail", `[{"retryDelay":{}},{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"2s"}]`, 2 * time.Second},
		{"no details", `[]`, 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"error":{"details":%s}}`, testCase.details)
			failure := fmt.Errorf("upstream: %w", &Error{StatusCode: http.StatusTooManyRequests, Body: []byte(body)})
			delay, ok := ParseGoogleRetryInfo(failure)
			require.Equal(t, testCase.want > 0, ok)
			require.Equal(t, testCase.want, delay)
		})
	}
	for _, body := range []string{`{"error":`, `{"error":{"details":"invalid"}}`} {
		delay, ok := ParseGoogleRetryInfo(&Error{StatusCode: http.StatusTooManyRequests, Body: []byte(body)})
		require.False(t, ok)
		require.Zero(t, delay)
	}
	delay, ok := ParseGoogleRetryInfo(&Error{
		StatusCode: http.StatusBadRequest,
		Body:       []byte(`{"error":{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"39s"}]}}`),
	})
	require.False(t, ok)
	require.Zero(t, delay)
}
