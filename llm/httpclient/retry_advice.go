package httpclient

import (
	"math"
	"net/http"
	"strconv"
	"strings"
)

// RetryAdviceHeaders retains only unambiguous, valid timing advice. It does not
// classify errors or cap delays; each consumer owns its retry policy and budget.
func RetryAdviceHeaders(headers http.Header) http.Header {
	canonical := make(http.Header)
	for name, values := range headers {
		switch strings.ToLower(name) {
		case "retry-after", "retry-after-ms", "x-ms-retry-after-ms":
			canonical[http.CanonicalHeaderKey(name)] = append(canonical[http.CanonicalHeaderKey(name)], values...)
		}
	}
	result := make(http.Header)
	for name, values := range canonical {
		if len(values) != 1 || strings.ContainsAny(values[0], "\r\n") {
			continue
		}
		value := strings.TrimSpace(values[0])
		if len(value) == 0 || len(value) > 128 {
			continue
		}
		if name == "Retry-After" {
			if date, err := http.ParseTime(value); err == nil {
				result.Set(name, date.UTC().Format(http.TimeFormat))
				continue
			}
		}
		amount, err := strconv.ParseFloat(value, 64)
		if err == nil && !math.IsNaN(amount) && !math.IsInf(amount, 0) && amount >= 0 {
			result.Set(name, strconv.FormatFloat(amount, 'f', -1, 64))
		}
	}
	return result
}
