package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/orchestrator"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestUpstreamRetryAdviceValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
		want   string
	}{
		{"seconds", []string{" 12 "}, "12"},
		{"fraction", []string{"0.25"}, "0.25"},
		{"zero", []string{"0"}, "0"},
		{"long delay is not shortened", []string{"7200"}, "7200"},
		{"date", []string{"Wed, 07 Oct 2026 16:00:00 GMT"}, "Wed, 07 Oct 2026 16:00:00 GMT"},
		{"negative", []string{"-1"}, ""},
		{"nan", []string{"NaN"}, ""},
		{"infinite", []string{"+Inf"}, ""},
		{"injection", []string{"12\r\nX-Secret: value"}, ""},
		{"surrounding line breaks", []string{"\n12\n"}, ""},
		{"duplicate", []string{"1", "2"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := &httpclient.Error{StatusCode: 503, Headers: http.Header{"retry-after": tc.values, "Authorization": {"secret"}, "Set-Cookie": {"secret"}}}
			got := upstreamRetryAdvice(raw, 503)
			require.Equal(t, tc.want, got.Get("Retry-After"))
			require.Empty(t, got.Get("Authorization"))
			require.Empty(t, got.Get("Set-Cookie"))
		})
	}
	for _, code := range []string{"quota_exhausted", "insufficient_quota", "usage_limit_reached", "billing_hard_limit_reached", "policy_violation"} {
		raw := &httpclient.Error{StatusCode: 429, Headers: http.Header{"Retry-After": {"3"}}, Body: []byte(fmt.Sprintf(`{"error":{"code":%q}}`, code))}
		require.Empty(t, upstreamRetryAdvice(raw, 429), code)
	}
	for _, status := range []int{400, 401, 403, 500} {
		raw := &httpclient.Error{StatusCode: status, Headers: http.Header{"Retry-After": {"3"}}}
		require.Empty(t, upstreamRetryAdvice(raw, status))
	}
	require.Empty(t, upstreamRetryAdvice(&llm.ResponseError{StatusCode: 503, Detail: llm.ErrorDetail{Code: "queue_full"}}, 503))
	require.Empty(t, upstreamRetryAdvice(orchestrator.NewQuotaExhaustedError("gpt-6.1-sol"), 503))
}

func TestRetryAdviceSurvivesErrorPolicyAndDownstreamTransports(t *testing.T) {
	for _, mode := range []string{biz.UpstreamErrorModePassthrough, biz.UpstreamErrorModeHidden, biz.UpstreamErrorModeCustom} {
		t.Run(mode, func(t *testing.T) {
			ctx, svc := setupUpstreamErrorPolicyTest(t, biz.UpstreamErrorPolicy{Mode: mode, CustomMessage: "safe failure"})
			orch := &orchestrator.ChatCompletionOrchestrator{Inbound: responses.NewInboundTransformer(), SystemService: svc}
			for _, status := range []int{429, 503} {
				t.Run(fmt.Sprint(status), func(t *testing.T) {
					raw := &httpclient.Error{
						StatusCode: status,
						Headers:    http.Header{"Retry-After": {"12"}, "Retry-After-Ms": {"12500"}, "X-Ms-Retry-After-Ms": {"13000"}, "Set-Cookie": {"private-cookie"}},
						Body:       []byte(`{"error":{"type":"server_error","code":"server_is_overloaded","message":"private upstream details"}}`),
					}
					failure := pipeline.WrapUpstreamError(&llm.ResponseError{
						StatusCode: status,
						Cause:      raw,
						Detail:     llm.ErrorDetail{Type: "server_error", Code: "server_is_overloaded", Message: "private upstream details"},
					})
					httpErr := transformOrchestratorError(ctx, failure, orch)
					require.Equal(t, status, httpErr.StatusCode)
					expectedMessage := "private upstream details"
					if mode == biz.UpstreamErrorModeHidden {
						expectedMessage = biz.DefaultUpstreamErrorMessage
					} else if mode == biz.UpstreamErrorModeCustom {
						expectedMessage = "safe failure"
					}
					w := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(w)
					writeOrchestratorHTTPError(c, httpErr)
					require.Equal(t, status, w.Code)
					require.Equal(t, "12", w.Header().Get("Retry-After"))
					require.Equal(t, "12500", w.Header().Get("Retry-After-Ms"))
					require.Empty(t, w.Header().Get("Set-Cookie"))
					require.Equal(t, expectedMessage, gjson.GetBytes(w.Body.Bytes(), "error.message").String())

					_, encode := newResponsesStreamAdapter(ctx, &errorAfterStream{}, svc)
					event, err := encode(ctx, failure)
					require.NoError(t, err)
					require.Equal(t, int64(status), gjson.GetBytes(event.Data, "status").Int())
					require.Equal(t, "12", gjson.GetBytes(event.Data, "headers.retry-after").String())
					require.Equal(t, "response.failed", event.Type)
					require.Equal(t, "failed", gjson.GetBytes(event.Data, "response.status").String())
					require.Equal(t, "12", gjson.GetBytes(event.Data, "response.error.headers.retry-after").String())
					require.Equal(t, "13000", gjson.GetBytes(event.Data, "response.error.headers.x-ms-retry-after-ms").String())
					require.Equal(t, expectedMessage, gjson.GetBytes(event.Data, "response.error.message").String())
					require.Equal(t, "server_is_overloaded", gjson.GetBytes(event.Data, "code").String())
					require.Equal(t, expectedMessage, gjson.GetBytes(event.Data, "message").String())
					require.NotContains(t, string(event.Data), "private-cookie")

					server := newResponsesWebSocketTestServer(t, func(context.Context, *httpclient.Request) (orchestrator.ChatCompletionResult, error) {
						return orchestrator.ChatCompletionResult{}, failure
					}, func(context.Context, error) *httpclient.Error { return transformOrchestratorError(ctx, failure, orch) })
					conn := dialResponsesWebSocket(t, server.URL, nil)
					defer conn.Close()
					require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","stream_id":"program-test","model":"gpt-6.1-sol","input":"hello"}`)))
					_, data, err := conn.ReadMessage()
					require.NoError(t, err)
					require.Equal(t, int64(status), gjson.GetBytes(data, "status").Int())
					require.Equal(t, "program-test", gjson.GetBytes(data, "stream_id").String())
					require.Equal(t, "12", gjson.GetBytes(data, "headers.retry-after").String())
					require.Equal(t, "13000", gjson.GetBytes(data, "headers.x-ms-retry-after-ms").String())
					require.Equal(t, expectedMessage, gjson.GetBytes(data, "error.message").String())
					require.NotContains(t, string(data), "private-cookie")
				})
			}
		})
	}
}
