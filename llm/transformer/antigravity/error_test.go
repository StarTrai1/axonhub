package antigravity

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/gemini"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestTransformError_SanitizesClientIdentityAndPreservesCause(t *testing.T) {
	adapter, err := NewTransformer(Config{APIKey: "synthetic", Project: "fixture"})
	require.NoError(t, err)
	cases := []struct {
		name    string
		body    string
		message string
		code    string
	}{
		{
			name:    "structured",
			body:    `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"Quota exceeded for projects/private-project by pool@private-project.iam.gserviceaccount.com; consumer: 123456789012","details":[{"unrecognized_identity":"private-detail"}]}}`,
			message: "Quota exceeded for projects/*** by ***; consumer: ***",
			code:    "429",
		},
		{
			name:    "wrapped",
			body:    `{"response":{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"Retry projects/private-project after 30 seconds"}}}`,
			message: "Retry projects/*** after 30 seconds",
			code:    "429",
		},
		{
			name:    "plain text",
			body:    `Project number '123456789012' rejected https://example.invalid/?key=private-key&retry=30&access_token=private-token for pool@example.invalid`,
			message: `Project number '***' rejected https://example.invalid/?key=***&retry=30&access_token=*** for ***`,
		},
		{
			name:    "missing message",
			body:    `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED","details":[{"unknown_account_field":"private-detail"}]}}`,
			message: "Too Many Requests",
			code:    "429",
		},
		{
			name:    "unrecognized JSON",
			body:    `{"unknown_account_field":"private-detail"}`,
			message: "Too Many Requests",
		},
	}
	inbounds := []transformer.Inbound{
		openai.NewInboundTransformer(),
		responses.NewInboundTransformer(),
		anthropic.NewInboundTransformer(),
		gemini.NewInboundTransformer(),
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := &httpclient.Error{
				StatusCode: http.StatusTooManyRequests,
				Headers:    http.Header{"Retry-After": []string{"30"}},
				Body:       []byte(tc.body),
			}
			converted := adapter.TransformError(t.Context(), raw)
			require.Equal(t, http.StatusTooManyRequests, converted.StatusCode)
			require.Equal(t, tc.message, converted.Detail.Message)
			require.Equal(t, tc.code, converted.Detail.Code)
			if tc.code != "" {
				require.Equal(t, "RESOURCE_EXHAUSTED", converted.Detail.Type)
			}
			wrapped := fmt.Errorf("provider: %w", converted)
			var original *httpclient.Error
			require.ErrorAs(t, wrapped, &original)
			require.Same(t, raw, original)
			require.Equal(t, tc.body, string(original.Body))
			require.Equal(t, "30", original.Headers.Get("Retry-After"))
			for _, inbound := range inbounds {
				clientErr := inbound.TransformError(t.Context(), wrapped)
				require.Equal(t, http.StatusTooManyRequests, clientErr.StatusCode)
				var payload struct {
					Error struct {
						Message string `json:"message"`
					} `json:"error"`
				}
				require.NoError(t, json.Unmarshal(clientErr.Body, &payload))
				require.Equal(t, tc.message, payload.Error.Message)
				require.NotContains(t, string(clientErr.Body), "private-")
				require.NotContains(t, string(clientErr.Body), "123456789012")
			}
		})
	}

	nilError := adapter.TransformError(t.Context(), nil)
	require.Equal(t, http.StatusInternalServerError, nilError.StatusCode)
	require.Nil(t, nilError.Cause)
	_, err = adapter.TransformResponse(t.Context(), &httpclient.Response{
		StatusCode: http.StatusForbidden,
		Body:       []byte(`{"error":{"code":403,"status":"PERMISSION_DENIED","message":"Access denied for projects/private-project"}}`),
	})
	require.ErrorContains(t, err, "Access denied for projects/***")
	require.NotContains(t, err.Error(), "private-project")
}
