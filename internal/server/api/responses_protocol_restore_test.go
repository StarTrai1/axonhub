package api

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
)

func TestUnifiedHistoryProtocolLostNativeContinuationReplaysAcceptedCheckpoint(t *testing.T) {
 for _, fresh := range []bool{false, true} {
  t.Run(map[bool]string{false: "upstream-lost", true: "new-client-connection"}[fresh], func(t *testing.T) { testUnifiedHistoryLostNativeContinuation(t, fresh) })
 }
}

func testUnifiedHistoryLostNativeContinuation(t *testing.T, fresh bool) {
 t.Helper()
	var calls atomic.Int32
	frames := make(chan []byte, 4)
	upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		for {
			_, body, err := conn.ReadMessage()
			if err != nil {
				return
			}
			frames <- body
			n := calls.Add(1)
			response := `{"type":"response.completed","response":{"id":"resp_accepted","status":"completed","output":[{"type":"function_call","id":"fc_exact","call_id":"call_exact","name":"inspect","arguments":"{}"}]}}`
			if n > 1 {
				response = `{"type":"response.completed","response":{"id":"resp_restored","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"continued without another summary"}]}]}}`
				if gjson.GetBytes(body, "previous_response_id").Exists() {
					response = `{"type":"error","error":{"code":"previous_response_not_found","type":"invalid_request_error","message":"Previous response not found"}}`
				}
			}
			if err := conn.WriteMessage(websocket.TextMessage, []byte(response)); err != nil {
				return
			}
		}
	}))
	client, handlers, project := newResponsesHistoryIntegrationFixture(t, upstream.URL, true, func(create *ent.ChannelCreate) {
		create.SetType(channel.TypeCodex).SetEndpoints([]objects.ChannelEndpoint{{APIFormat: "openai/responses-ws"}}).
			SetSettings(&objects.ChannelSettings{PassThroughBody: lo.ToPtr(true), ModelProtocols: []objects.ModelProtocol{{Model: "synthetic-model", APIFormats: []string{"openai/responses-ws"}}}})
	})
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	_, err := client.DataStorage.Create().SetName("primary").SetDescription("synthetic recovery history").SetPrimary(true).SetType("database").SetSettings(new(objects.DataStorageSettings)).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, handlers.SystemService.SetSecretKey(ctx, "synthetic-restore-secret"))
	gateway := responsesIntegrationGateway(t, client, handlers, project)
	conn := responsesIntegrationDial(t, gateway)
	first := responsesIntegrationTurn(t, conn, `{"type":"response.create","model":"synthetic-model","input":[{"role":"user","content":"exact task"},{"type":"compaction","id":"cmp_accepted","encrypted_content":"accepted-cipher"}]}`)
	require.Equal(t, "resp_accepted", gjson.GetBytes(first, "response.id").String(), string(first))
	<-frames
 if fresh { require.NoError(t, conn.Close()); conn = responsesIntegrationDial(t, gateway) }
	second := responsesIntegrationTurn(t, conn, `{"type":"response.create","model":"synthetic-model","previous_response_id":"resp_accepted","input":[{"type":"function_call_output","call_id":"call_exact","output":"exact result"}]}`)
	require.Equal(t, "resp_restored", gjson.GetBytes(second, "response.id").String(), string(second))
	want := int32(2)
 if !fresh { want = 3; delta := <-frames; require.Equal(t, "resp_accepted", gjson.GetBytes(delta, "previous_response_id").String()) }
 require.Equal(t, want, calls.Load(), "one full correction; no summary generation")
 full := <-frames
	require.False(t, gjson.GetBytes(full, "previous_response_id").Exists())
	require.Len(t, gjson.GetBytes(full, "input").Array(), 4)
	require.Equal(t, "accepted-cipher", gjson.GetBytes(full, "input.1.encrypted_content").String())
	require.Equal(t, "exact result", gjson.GetBytes(full, "input.3.output").String())
}
