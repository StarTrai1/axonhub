package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
)

func codexMemoryIntegrationBody(t *testing.T, kind, input string, stream bool) string {
	t.Helper()
	metadata, err := json.Marshal(map[string]string{"request_kind": kind, "thread_source": map[bool]string{true: "memory_consolidation", false: "user"}[kind == "memory"]})
	require.NoError(t, err)
	body, err := json.Marshal(map[string]any{
		"type": "response.create", "model": "synthetic-model", "stream": stream, "input": input,
		"client_metadata": map[string]string{"thread_id": t.Name(), "x-codex-turn-metadata": string(metadata)},
	})
	require.NoError(t, err)
	return string(body)
}

func TestCodexMemoryForegroundFailureCancelsAndBlocksBackground(t *testing.T) {
	for _, passThrough := range []bool{true, false} {
		for _, lateFailure := range []bool{false, true} {
			t.Run(fmt.Sprintf("raw=%t/late=%t", passThrough, lateFailure), func(t *testing.T) {
				memoryCanceled := make(chan struct{})
				var memoryRequests atomic.Int32
				upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := readRequestBody(r)
					if err != nil {
						t.Error(err)
						return
					}
					if strings.Contains(string(body), "memory-") {
						memoryRequests.Add(1)
					}
					// Both pass-through and converted inputs retain these canaries.
					text := string(body)
					if strings.Contains(text, "memory-running") {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_memory\",\"status\":\"in_progress\"}}\n\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_memory\",\"item_id\":\"msg_memory\",\"output_index\":0,\"content_index\":0,\"delta\":\"working\"}\n\n")
						w.(http.Flusher).Flush()
						<-r.Context().Done()
						close(memoryCanceled)
						return
					}
					if strings.Contains(text, "foreground-fail") {
						if !lateFailure {
							w.WriteHeader(http.StatusBadRequest)
							_, _ = io.WriteString(w, `{"error":{"code":"synthetic_failure","type":"invalid_request_error","message":"foreground failed"}}`)
							return
						}
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_foreground\",\"status\":\"in_progress\"}}\n\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"resp_foreground\",\"item_id\":\"msg_foreground\",\"output_index\":0,\"content_index\":0,\"delta\":\"partial\"}\n\ndata: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_foreground\",\"status\":\"failed\",\"error\":{\"code\":\"synthetic_failure\",\"message\":\"foreground failed\"}}}\n\n")
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_resumed\",\"object\":\"response\",\"model\":\"synthetic-model\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"resumed\"}]}]}}\n\n")
				}))
				client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL, func(create *ent.ChannelCreate) {
					create.SetType(channel.TypeCodex).SetSettings(&objects.ChannelSettings{PassThroughBody: lo.ToPtr(passThrough)})
				})
				gateway := responsesIntegrationGateway(t, client, handlers, project)
				memory := responsesIntegrationDial(t, gateway)
				require.NoError(t, memory.SetReadDeadline(time.Now().Add(10*time.Second)))
				require.NoError(t, memory.WriteMessage(websocket.TextMessage, []byte(codexMemoryIntegrationBody(t, "memory", "memory-running", true))))
				_, event, err := memory.ReadMessage()
				require.NoError(t, err)
				require.Equal(t, "response.created", gjson.GetBytes(event, "type").String(), string(event))
				foreground := responsesIntegrationDial(t, gateway)
				event = responsesIntegrationTurn(t, foreground, codexMemoryIntegrationBody(t, "turn", "foreground-fail", true))
				require.Contains(t, []string{"error", "response.failed"}, gjson.GetBytes(event, "type").String(), string(event))
				responsesIntegrationAwait(t, memoryCanceled)
				status, blocked := responsesIntegrationPost(t, gateway, "/v1/responses", codexMemoryIntegrationBody(t, "memory", "memory-blocked", false))
				require.Equal(t, http.StatusBadRequest, status, string(blocked))
				require.Equal(t, "codex_memory_paused", gjson.GetBytes(blocked, "error.code").String(), string(blocked))
				require.Equal(t, int32(1), memoryRequests.Load(), "blocked memory must never reach upstream")
				status, resumed := responsesIntegrationPost(t, gateway, "/v1/responses", codexMemoryIntegrationBody(t, "turn", "foreground-success", false))
				require.Equal(t, http.StatusOK, status, string(resumed))
				status, resumed = responsesIntegrationPost(t, gateway, "/v1/responses", codexMemoryIntegrationBody(t, "memory", "memory-resumed", false))
				require.Equal(t, http.StatusOK, status, string(resumed))
				require.Equal(t, int32(2), memoryRequests.Load())
			})
		}
	}
}
