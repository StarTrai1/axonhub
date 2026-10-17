package api

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/datastorage"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/objects"
)

// A provider completion is not sufficient: the real handlers must acknowledge
// the terminal write through every decorator, persist it, and accept the next
// continuation. Live preview used to hide that acknowledgement on both routes.
func TestStreamDeliveryCodexLivePreview(t *testing.T) {
	for _, model := range []string{"gpt-6-astra", "gpt-6.1-sol"} {
		for _, upstreamWS := range []bool{false, true} {
			for _, downstream := range []string{"http", "ws"} {
				for _, passthrough := range []bool{false, true} {
					for _, preview := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/upstream_ws=%t/%s/passthrough=%t/preview=%t", model, upstreamWS, downstream, passthrough, preview), func(t *testing.T) {
							runCodexPreviewDelivery(t, model, upstreamWS, downstream, passthrough, preview, "completed")
						})
					}
				}
			}
		}
	}
}

func TestStreamDeliveryCodexLivePreviewFailure(t *testing.T) {
	for _, status := range []string{"failed", "incomplete"} {
		for _, downstream := range []string{"http", "ws"} {
			for _, passthrough := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/passthrough=%t", status, downstream, passthrough), func(t *testing.T) {
					runCodexPreviewDelivery(t, "gpt-6.1-sol", false, downstream, passthrough, true, status)
				})
			}
		}
	}
}

func runCodexPreviewDelivery(t *testing.T, model string, upstreamWS bool, downstream string, passthrough, preview bool, terminalStatus string) {
	t.Helper()
	var calls atomic.Int32
	frames := func() []string {
		turn := calls.Add(1)
		id := fmt.Sprintf("resp_preview_%d", turn)
		output := `[{"type":"message","id":"msg_preview","role":"assistant","status":"completed","content":[{"type":"output_text","text":"preview-answer","annotations":[]}]}]`
		details := ""
		if terminalStatus == "failed" {
			details = `,"error":{"code":"server_error","message":"synthetic provider failure"}`
		} else if terminalStatus == "incomplete" {
			details = `,"incomplete_details":{"reason":"max_output_tokens"}`
		}
		return []string{
			fmt.Sprintf(`{"type":"response.created","sequence_number":0,"response":{"id":%q,"object":"response","created_at":1700000000,"model":%q,"status":"in_progress","output":[]}}`, id, model),
			`{"type":"response.output_item.added","sequence_number":1,"output_index":0,"item":{"type":"message","id":"msg_preview","role":"assistant","status":"in_progress","content":[]}}`,
			`{"type":"response.output_text.delta","sequence_number":2,"output_index":0,"content_index":0,"item_id":"msg_preview","delta":"preview-answer"}`,
			fmt.Sprintf(`{"type":"response.output_item.done","sequence_number":3,"output_index":0,"item":%s}`, gjson.Get(output, "0").Raw),
			fmt.Sprintf(`{"type":"response.%s","sequence_number":4,"response":{"id":%q,"object":"response","created_at":1700000000,"model":%q,"status":%q,"output":%s,"usage":{"input_tokens":12,"output_tokens":34,"total_tokens":46}%s}}`, terminalStatus, id, model, terminalStatus, output, details),
		}
	}
	upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if upstreamWS {
			conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
				for _, event := range frames() {
					if err := conn.WriteMessage(websocket.TextMessage, []byte(event)); err != nil {
						return
					}
				}
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range frames() {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
		}
	}))
	client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL, func(create *ent.ChannelCreate) {
		create.SetType(channel.TypeCodex).SetSupportedModels([]string{"synthetic-model", model}).
			SetSettings(&objects.ChannelSettings{PassThroughBody: lo.ToPtr(passthrough)})
		if upstreamWS {
			create.SetEndpoints([]objects.ChannelEndpoint{{APIFormat: "openai/responses-ws"}})
		}
	})
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	storage, err := client.DataStorage.Create().SetName("preview-storage").SetDescription("Synthetic delivery regression storage").SetPrimary(true).
		SetType(datastorage.TypeDatabase).SetSettings(&objects.DataStorageSettings{}).SetStatus(datastorage.StatusActive).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, handlers.SystemService.SetDefaultDataStorageID(ctx, storage.ID))
	policy := *handlers.SystemService.StoragePolicyOrDefault(ctx)
	policy.LivePreview = preview
	policy.StoreResponseBody = true
	require.NoError(t, handlers.SystemService.SetStoragePolicy(ctx, &policy))
	gateway := responsesIntegrationGateway(t, client, handlers, project)
	turns := 1
	if terminalStatus == "completed" {
		turns = 2
	}
	var conn *websocket.Conn
	if downstream == "ws" {
		conn = responsesIntegrationDial(t, gateway)
	}
	for turn := 1; turn <= turns; turn++ {
		previous := ""
		if turn > 1 {
			previous = `,"previous_response_id":"resp_preview_1"`
		}
		body := fmt.Sprintf(`{"model":%q,"stream":true,"input":"preview-question"%s}`, model, previous)
		if conn == nil {
			status, response := responsesIntegrationPost(t, gateway, "/v1/responses", body)
			require.Equal(t, http.StatusOK, status, string(response))
			require.Contains(t, string(response), `"type":"response.`+terminalStatus+`"`)
		} else {
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
			require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create",`+body[1:])))
			for {
				_, event, err := conn.ReadMessage()
				require.NoError(t, err)
				kind := gjson.GetBytes(event, "type").String()
				require.NotEqual(t, "error", kind, string(event))
				if kind == "response."+terminalStatus {
					break
				}
			}
		}
	}
	if conn != nil {
		// Exercise cancellation immediately after the final terminal write.
		require.NoError(t, conn.Close())
	}
	want := request.StatusCompleted
	if terminalStatus != "completed" {
		want = request.StatusFailed
	}
	require.Eventually(t, func() bool {
		rows, err := client.Request.Query().All(ctx)
		if err != nil || len(rows) != turns {
			return false
		}
		for _, row := range rows {
			if row.Status != want {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond, "provider completed but delivery status was lost")
	executions, err := client.RequestExecution.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, executions, turns)
	for _, execution := range executions {
		require.Equal(t, requestexecution.Status(want), execution.Status)
	}
	logs, err := client.UsageLog.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, logs, turns)
	for _, usage := range logs {
		require.EqualValues(t, 12, usage.PromptTokens)
		require.EqualValues(t, 34, usage.CompletionTokens)
		require.EqualValues(t, 46, usage.TotalTokens)
	}
	require.EqualValues(t, turns, calls.Load(), "no hidden replay after a successful terminal")
}
