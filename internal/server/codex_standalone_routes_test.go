package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
)

func TestCodexRoutes_StandaloneRequests(t *testing.T) {
	for _, tc := range []struct {
		path, request, response, resultPath, expected string
		format                                        llm.APIFormat
	}{
		{"/alpha/search", `{"model":"gpt-6-sol","commands":{"search_query":[{"q":"fixture"}]}}`, `{"encrypted_output":"opaque","output":"fixture result","results":[]}`, "output", "fixture result", llm.APIFormatOpenAISearch},
		{"/responses/compact", `{"model":"gpt-6-sol","input":"fixture"}`, `{"id":"cmp_fixture","object":"response.compaction","model":"gpt-6-sol","output":[{"type":"compaction","id":"cmp_item","encrypted_content":"fixture-checkpoint"}]}`, "output.0.type", "compaction", llm.APIFormatOpenAIResponseCompact},
	} {
		t.Run(tc.path, func(t *testing.T) {
			h := newCodexRouteHarness(t)
			called := make(chan struct{}, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				assert.True(t, strings.HasSuffix(r.URL.Path, tc.path), r.URL.Path)
				assert.Equal(t, "Bearer upstream", r.Header.Get("Authorization"))
				assert.Equal(t, "gpt-6-sol", gjson.GetBytes(body, "model").String())
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.response)
				called <- struct{}{}
			}))
			t.Cleanup(upstream.Close)
			h.source.Update().SetBaseURL(upstream.URL).SetSupportedModels([]string{"gpt-6-sol"}).
				SetSettings(&objects.ChannelSettings{CodexSearchOnly: tc.format == llm.APIFormatOpenAISearch}).
				SetEndpoints([]objects.ChannelEndpoint{{APIFormat: tc.format.String()}}).SaveX(h.ctx)
			channel, err := h.channels.GetChannel(h.ctx, h.source.ID)
			require.NoError(t, err)
			h.channels.SetEnabledChannelsForTest([]*biz.Channel{channel})
			require.NoError(t, h.system.SetCodexCompatibilitySettings(h.ctx, biz.CodexCompatibilitySettings{Enabled: true, ChannelID: &h.source.ID}))
			req := httptest.NewRequest(http.MethodPost, "/codex"+tc.path, strings.NewReader(tc.request))
			req.Header.Set("Authorization", "Bearer "+h.key.Key)
			req.Header.Set("Content-Type", "application/json")
			result := httptest.NewRecorder()
			h.router.ServeHTTP(result, req)
			require.Equal(t, http.StatusOK, result.Code, result.Body.String())
			require.Equal(t, tc.expected, gjson.Get(result.Body.String(), tc.resultPath).String())
			require.Len(t, called, 1)
		})
	}
}
