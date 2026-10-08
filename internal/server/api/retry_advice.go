package api

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/looplj/axonhub/internal/server/orchestrator"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

// upstreamRetryAdvice copies only validated timing hints from an actual upstream
// 429/503. It does not alter retry budgets or promote a local error to retryable.
// Long delays are retained: the downstream client owns its own wait budget.
func upstreamRetryAdvice(err error, status int) http.Header {
	if status != http.StatusTooManyRequests && status != http.StatusServiceUnavailable {
		return nil
	}
	var quotaErr *orchestrator.QuotaExhaustedError
	if errors.As(err, &quotaErr) {
		return nil
	}
	var raw *httpclient.Error
	if !errors.As(err, &raw) || raw == nil || raw.StatusCode != status {
		return nil
	}
	code := upstreamErrorCodeFromHTTP(raw)
	typeName := upstreamErrorTypeFromHTTP(raw)
	var responseErr *llm.ResponseError
	if errors.As(err, &responseErr) {
		code = firstNonEmpty(responseErr.Detail.Code, code)
		typeName = firstNonEmpty(responseErr.Detail.Type, typeName)
	}
	for _, value := range []string{code, typeName} {
		switch strings.ToLower(value) {
		case errCodeQuotaExhausted, "insufficient_quota", "usage_limit_reached", "billing_hard_limit_reached",
			"billing_hard_limit", "credit_balance_exhausted", "organization_spend_limit_exceeded",
			"project_spend_limit_exceeded", "organization_usage_limit_exceeded", "policy_violation",
			"content_policy_violation", "access_program_not_enabled":
			return nil
		}
	}

	result := make(http.Header)
	for name, values := range raw.Headers {
		switch strings.ToLower(name) {
		case "retry-after", "retry-after-ms", "x-ms-retry-after-ms":
		default:
			continue
		}
		if len(values) != 1 {
			continue
		}
		value := strings.TrimSpace(values[0])
		if len(value) == 0 || len(value) > 128 {
			continue
		}
		if strings.EqualFold(name, "Retry-After") {
			if date, parseErr := http.ParseTime(value); parseErr == nil {
				result.Set(name, date.UTC().Format(http.TimeFormat))
				continue
			}
		}
		amount, parseErr := strconv.ParseFloat(value, 64)
		if parseErr == nil && !math.IsNaN(amount) && !math.IsInf(amount, 0) && amount >= 0 {
			result.Set(name, strconv.FormatFloat(amount, 'f', -1, 64))
		}
	}
	return result
}

func retryAdviceEventHeaders(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	result := make(map[string]string, len(headers))
	for name := range headers {
		result[strings.ToLower(name)] = headers.Get(name)
	}
	return result
}

func writeOrchestratorHTTPError(c *gin.Context, httpErr *httpclient.Error) {
	for name, values := range upstreamRetryAdvice(httpErr, httpErr.StatusCode) {
		c.Header(name, values[0])
	}
	c.JSON(httpErr.StatusCode, json.RawMessage(httpErr.Body))
}
