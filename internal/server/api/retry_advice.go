package api

import (
	"encoding/json"
	"errors"
	"net/http"
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
	if _, ok := errors.AsType[*orchestrator.QuotaExhaustedError](err); ok {
		return nil
	}
	var raw *httpclient.Error
	if !errors.As(err, &raw) || raw == nil || raw.StatusCode != status {
		return nil
	}
	code := upstreamErrorCodeFromHTTP(raw)
	typeName := upstreamErrorTypeFromHTTP(raw)
	if responseErr, ok := errors.AsType[*llm.ResponseError](err); ok {
		code = firstNonEmpty(responseErr.Detail.Code, code)
		typeName = firstNonEmpty(responseErr.Detail.Type, typeName)
	}
	for _, value := range []string{code, typeName} {
		switch strings.ToLower(value) {
		case errCodeQuotaExhausted, "insufficient_quota", "usage_limit_reached", "billing_hard_limit_reached",
			"billing_hard_limit", "credit_balance_exhausted", "organization_spend_limit_exceeded",
			"project_spend_limit_exceeded", "organization_usage_limit_exceeded", "policy_violation",
			"content_policy_violation", "access_program_not_enabled", "misalignment_policy_violation", "cyber_policy", "bio_policy", "flex_unavailable":
			return nil
		}
	}

	return httpclient.RetryAdviceHeaders(raw.Headers)
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
