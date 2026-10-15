package antigravity

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestAntigravityTieredCatalogRequests(t *testing.T) {
	client := httpclient.NewHttpClientWithClient(&http.Client{Transport: &mockRoundTripper{roundTrip: func(req *http.Request) (*http.Response, error) {
		require.Equal(t, TokenURL, req.URL.String())
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"access_token":"synthetic-token","expires_in":3600}`))}, nil
	}}})
	adapter, err := NewTransformer(Config{BaseURL: EndpointDaily, APIKey: "synthetic-refresh|synthetic-project"}, WithHTTPClient(client))
	require.NoError(t, err)
	for _, tc := range []struct{ model, effort, want, tier string }{
		{"gemini-4-argon", "", "gemini-4-argon-high", "high"},
		{"gemini-4-argon", "medium", "gemini-4-argon-medium", "medium"},
		{"gemini-4-argon", "low", "gemini-4-argon-low", "low"},
		{"gemini-4-argon-low", "high", "gemini-4-argon-low", "low"},
		{"antigravity-gemini-4-argon:antigravity", "medium", "gemini-4-argon-medium", "medium"},
		{"gemini-4-argon-cyber-permissive", "high", "gemini-4-argon-cyber-permissive", ""},
		{"claude-opus-5-5", "", "claude-opus-5-5-high", ""},
		{"claude-sonnet-5-5", "medium", "claude-sonnet-5-5-medium", ""},
		{"claude-sonnet-5-5-low", "high", "claude-sonnet-5-5-low", ""},
		{"gemini-2.5-flash", "low", "gemini-2.5-flash", ""},
	} {
		t.Run(tc.model+"/"+tc.effort, func(t *testing.T) {
			req := &llm.Request{Model: tc.model, ReasoningEffort: tc.effort, Messages: []llm.Message{{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("keep request")}}}}
			original, err := json.Marshal(req)
			require.NoError(t, err)
			wire, err := adapter.TransformRequest(t.Context(), req)
			require.NoError(t, err)
			require.Equal(t, tc.want, gjson.GetBytes(wire.Body, "model").String())
			if tc.tier != "" {
				require.Equal(t, tc.tier, gjson.GetBytes(wire.Body, "request.generationConfig.thinkingConfig.thinkingLevel").String())
				require.False(t, gjson.GetBytes(wire.Body, "request.generationConfig.thinkingConfig.thinkingBudget").Exists())
			}
			after, err := json.Marshal(req)
			require.NoError(t, err)
			require.Equal(t, original, after)
		})
	}
}
