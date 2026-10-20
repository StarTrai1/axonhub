package api

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestUnifiedHistoryProtocolMemoryResumesAfterForegroundSuccess(t *testing.T) {
	var calls atomic.Int32
	upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"error":{"type":"invalid_request_error","code":"invalid_argument","message":"synthetic foreground failure"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_memory_%d\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"complete\"}]}]}}\n\n", n)
	}))
	client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL)
	gateway := responsesIntegrationGateway(t, client, handlers, project)
	body := func(kind string) string {
		return fmt.Sprintf(`{"model":"synthetic-model","stream":true,"input":"synthetic task","client_metadata":{"thread_id":"memory-recovery-integration","x-codex-turn-metadata":%q}}`, `{"request_kind":"`+kind+`"}`)
	}
	status, _ := responsesIntegrationPost(t, gateway, "/v1/responses", body("turn"))
	require.Equal(t, http.StatusBadRequest, status)
	status, response := responsesIntegrationPost(t, gateway, "/v1/responses", body("memory"))
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "codex_memory_paused", gjson.GetBytes(response, "error.code").String())
	require.Equal(t, int32(1), calls.Load(), "paused memory must not reach upstream")
	status, response = responsesIntegrationPost(t, gateway, "/v1/responses", body("turn"))
	require.Equal(t, http.StatusOK, status, string(response))
	require.Contains(t, string(response), "response.completed")
	status, response = responsesIntegrationPost(t, gateway, "/v1/responses", body("memory"))
	require.Equal(t, http.StatusOK, status, string(response))
	require.Contains(t, string(response), "response.completed")
	require.Equal(t, int32(3), calls.Load(), "a successful foreground delivery admits the next memory request")
}
