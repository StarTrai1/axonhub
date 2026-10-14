package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
)

func TestResponsesProtocolCompactionRelayLocalToNative(t *testing.T) {
	for _, transport := range []string{"openai/responses", "openai/responses-ws"} {
		for _, downstream := range []string{"http", "ws"} {
			t.Run(transport+"/"+downstream, func(t *testing.T) {
				var mu sync.Mutex
				var bodies [][]byte
				reply := func(body []byte) []byte {
					mu.Lock()
					defer mu.Unlock()
					bodies = append(bodies, body)
					return []byte(`{"type":"response.completed","response":{"id":"resp_local_source","object":"response","model":"gpt-6.1-sol","status":"completed","output":[{"type":"message","id":"msg_local_source","role":"assistant","content":[{"type":"output_text","text":"local-checkpoint-summary-canary"}]}],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}}`)
				}
				upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if transport == "openai/responses-ws" {
						conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
						if err != nil {
							t.Error(err)
							return
						}
						defer conn.Close()
						_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
						for {
							_, body, err := conn.ReadMessage()
							if err != nil {
								return
							}
							if err := conn.WriteMessage(websocket.TextMessage, reply(body)); err != nil {
								return
							}
						}
					}
					body, err := readRequestBody(r)
					if err != nil {
						t.Error(err)
						return
					}
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(w, "data: %s\n\n", reply(body))
				}))
				client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL, func(create *ent.ChannelCreate) {
					create.SetType(channel.TypeCodex).SetSupportedModels([]string{"synthetic-model", "gpt-6.1-sol"}).
						SetEndpoints([]objects.ChannelEndpoint{{APIFormat: transport}}).
						SetSettings(&objects.ChannelSettings{PassThroughBody: lo.ToPtr(true)}).
						SetPolicies(objects.ChannelPolicies{RemoteCompaction: objects.RemoteCompactionPolicyLocalBridge})
				})
				ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
				require.NoError(t, handlers.SystemService.SetSecretKey(ctx, "synthetic-local-to-native-secret"))
				gateway := responsesIntegrationGateway(t, client, handlers, project)
				var turn func(string) gjson.Result
				if downstream == "ws" {
					conn := responsesIntegrationDial(t, gateway)
					turn = func(body string) gjson.Result {
						var payload map[string]any
						require.NoError(t, json.Unmarshal([]byte(body), &payload))
						delete(payload, "stream")
						payload["type"] = "response.create"
						encoded, err := json.Marshal(payload)
						require.NoError(t, err)
						response := responsesIntegrationTurn(t, conn, string(encoded))
						require.Equal(t, "response.completed", gjson.GetBytes(response, "type").String(), string(response))
						return gjson.GetBytes(response, "response")
					}
				} else {
					turn = func(body string) gjson.Result {
						status, response := responsesIntegrationPost(t, gateway, "/v1/responses", body)
						require.Equal(t, http.StatusOK, status, string(response))
						// Inline compaction uses the existing streaming bridge even
						// when the HTTP caller did not set stream explicitly.
						if !gjson.ValidBytes(response) {
							for _, line := range strings.Split(string(response), "\n") {
								event := gjson.Parse(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
								if event.Get("type").String() == "response.completed" {
									return event.Get("response")
								}
							}
							t.Fatalf("missing completed compaction event: %s", response)
						}
						return gjson.ParseBytes(response)
					}
				}
				root := `{"model":"gpt-6.1-sol","stream":false,"client_metadata":{"thread_id":"local-thread"},"input":[{"role":"user","content":"original-local-source-canary"},{"type":"compaction_trigger"}]}`
				first := turn(root)
				checkpoint := first.Get(`output.#(type=="compaction")`)
				require.NotEmpty(t, checkpoint.Raw, first.Raw)
				require.Contains(t, checkpoint.Get("encrypted_content").String(), "axonhub-local-v2.")
				stored, err := client.Channel.Query().Only(ctx)
				require.NoError(t, err)
				_, err = handlers.ChannelService.UpdateChannel(ctx, stored.ID, &ent.UpdateChannelInput{Policies: &objects.ChannelPolicies{RemoteCompaction: objects.RemoteCompactionPolicyNative}})
				require.NoError(t, err)
				second := turn(fmt.Sprintf(`{"model":"gpt-6.1-sol","stream":false,"client_metadata":{"thread_id":"local-thread"},"input":[{"role":"user","content":"retained-local-user"},%s,{"role":"user","content":"after-switch-canary"}]}`, checkpoint.Raw))
				require.Equal(t, "completed", second.Get("status").String())
				mu.Lock()
				defer mu.Unlock()
				require.Len(t, bodies, 2)
				require.NotContains(t, string(bodies[0]), "compaction_trigger", "local generation must submit its summary prompt")
				require.Contains(t, string(bodies[0]), "CONTEXT CHECKPOINT COMPACTION")
				require.Contains(t, string(bodies[0]), "original-local-source-canary")
				require.Equal(t, "none", gjson.GetBytes(bodies[0], "tool_choice").String())
				require.NotContains(t, string(bodies[1]), "axonhub-local-v2.", "native providers cannot decrypt gateway checkpoints")
				require.False(t, gjson.GetBytes(bodies[1], `input.#(type=="compaction")`).Exists())
				require.Contains(t, string(bodies[1]), "local-checkpoint-summary-canary")
				require.Contains(t, string(bodies[1]), "retained-local-user")
				require.Contains(t, string(bodies[1]), "after-switch-canary")
			})
		}
	}
}
