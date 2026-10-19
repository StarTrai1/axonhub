package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/model"
	"github.com/looplj/axonhub/internal/objects"
)

func TestResponsesProtocolNativeCompactionMigrationFromWebSocketDelta(t *testing.T) {
	testResponsesProtocolHistoryMigration(t, false)
}

func TestUnifiedHistoryProtocolMigrationFromWebSocketDelta(t *testing.T) {
	testResponsesProtocolHistoryMigration(t, true)
}

func testResponsesProtocolHistoryMigration(t *testing.T, unified bool) {
	t.Helper()
	for _, policy := range []objects.RemoteCompactionPolicy{objects.RemoteCompactionPolicyAuto, objects.RemoteCompactionPolicyLocalBridge} {
		t.Run(string(policy), func(t *testing.T) {
			frames := make(chan []byte, 2)
			var mu sync.Mutex
			var migrated [][]byte
			upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if websocket.IsWebSocketUpgrade(r) {
					conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.Close()
					for turn := range 2 {
						_, body, err := conn.ReadMessage()
						if err != nil {
							t.Error(err)
							return
						}
						frames <- body
						response := `{"type":"response.completed","response":{"id":"resp_before_compaction","object":"response","status":"completed","output":[{"type":"custom_tool_call","id":"ct_original","call_id":"call_original","name":"inspect","input":"original exact arguments"}]}}`
						if turn == 1 {
							response = `{"type":"response.completed","response":{"id":"resp_original_compaction","object":"response","status":"completed","output":[{"type":"compaction","id":"cmp_ws_source","encrypted_content":"source-cipher"}]}}`
						}
						id := gjson.Get(response, "response.id").String()
						created := fmt.Sprintf(`{"type":"response.created","response":{"id":%q,"object":"response","model":"gpt-6-astra","status":"in_progress","output":[]}}`, id)
						if err := conn.WriteMessage(websocket.TextMessage, []byte(created)); err != nil {
							t.Error(err)
							return
						}
						if err := conn.WriteMessage(websocket.TextMessage, []byte(response)); err != nil {
							t.Error(err)
							return
						}
					}
					return
				}
				body, err := readRequestBody(r)
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				migrated = append(migrated, body)
				mu.Unlock()
				if gjson.GetBytes(body, `input.#(id=="cmp_ws_source")`).Exists() {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = fmt.Fprint(w, `{"error":{"type":"invalid_request_error","message":"The encrypted content for item cmp_ws_source could not be verified. Reason: Encrypted content could not be decrypted or parsed."}}`)
					return
				}
				output := `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"continued with preserved source"}]}]`
				if gjson.GetBytes(body, `input.#(type=="compaction_trigger")`).Exists() {
					output = `[{"type":"compaction","id":"cmp_http_destination","encrypted_content":"destination-cipher"}]`
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_destination\",\"object\":\"response\",\"status\":\"completed\",\"output\":%s}}\n\n", output)
			}))
			client, handlers, project := newResponsesHistoryIntegrationFixture(t, upstream.URL, unified, func(create *ent.ChannelCreate) {
				create.SetType(channel.TypeCodex).SetSupportedModels([]string{"synthetic-model", "gpt-6-astra"}).
					SetPolicies(objects.ChannelPolicies{RemoteCompaction: objects.RemoteCompactionPolicyNative}).
					SetEndpoints([]objects.ChannelEndpoint{{APIFormat: "openai/responses-ws"}}).
					SetSettings(&objects.ChannelSettings{PassThroughBody: lo.ToPtr(true), ModelProtocols: []objects.ModelProtocol{{Model: "gpt-6-astra", APIFormats: []string{"openai/responses-ws"}}}})
			})
			ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
			_, err := client.DataStorage.Create().SetName("primary").SetDescription("synthetic compaction history").SetPrimary(true).SetType("database").SetSettings(new(objects.DataStorageSettings)).Save(ctx)
			require.NoError(t, err)
			require.NoError(t, handlers.SystemService.SetSecretKey(ctx, "synthetic-delta-source-secret"))
			if unified {
				sourceChannel, err := client.Channel.Query().Only(ctx)
				require.NoError(t, err)
				_, err = client.Model.Create().SetDeveloper("openai").SetModelID("gpt-6-astra").SetName("gpt-6-astra").SetIcon("openai").SetGroup("gpt").SetStatus(model.StatusEnabled).
					SetModelCard(&objects.ModelCard{Limit: objects.ModelCardLimit{Context: 372000, Output: 8192}, ToolCall: true}).
					SetSettings(&objects.ModelSettings{Associations: []*objects.ModelAssociation{{Type: "channel_model", ChannelModel: &objects.ChannelModelAssociation{ChannelID: sourceChannel.ID, ModelID: "gpt-6-astra"}}}}).Save(ctx)
				require.NoError(t, err)
			}
			retries := handlers.SystemService.RetryPolicyOrDefault(ctx)
			retries.Enabled, retries.MaxChannelRetries, retries.MaxSingleChannelRetries, retries.RetryDelayMs = true, 0, 2, 0
			require.NoError(t, handlers.SystemService.SetRetryPolicy(ctx, retries))
			gateway := responsesIntegrationGateway(t, client, handlers, project)
			conn := responsesIntegrationDial(t, gateway)
			first := responsesIntegrationTurn(t, conn, `{"type":"response.create","model":"gpt-6-astra","client_metadata":{"thread_id":"delta-thread"},"input":[{"role":"user","content":"complete original source canary"}]}`)
			require.Equal(t, "resp_before_compaction", gjson.GetBytes(first, "response.id").String(), string(first))
			<-frames
			compacted := responsesIntegrationTurn(t, conn, `{"type":"response.create","model":"gpt-6-astra","previous_response_id":"resp_before_compaction","client_metadata":{"thread_id":"delta-thread","x-codex-turn-metadata":"{\"request_kind\":\"compaction\",\"compaction\":{\"implementation\":\"responses_compaction_v2\"}}"},"input":[{"type":"custom_tool_call_output","call_id":"call_original","output":"original exact tool result"},{"type":"compaction_trigger"}]}`)
			if gjson.GetBytes(compacted, "response.output.0.id").String() != "cmp_ws_source" {
				rows, err := client.Request.Query().All(ctx)
				require.NoError(t, err)
				for _, row := range rows {
					t.Logf("source fixture row id=%d external=%s status=%s format=%s response=%s", row.ID, row.ExternalID, row.Status, row.Format, row.ResponseBody)
					executions, err := row.QueryExecutions().All(ctx)
					require.NoError(t, err)
					for _, execution := range executions {
						t.Logf("source fixture execution status=%s format=%s response=%s", execution.Status, execution.Format, execution.ResponseBody)
					}
				}
			}
			require.Equal(t, "cmp_ws_source", gjson.GetBytes(compacted, "response.output.0.id").String(), string(compacted))
			delta := <-frames
			require.Equal(t, "resp_before_compaction", gjson.GetBytes(delta, "previous_response_id").String())
			require.Len(t, gjson.GetBytes(delta, "input").Array(), 2, "the source is an actual connection-local delta")
			if unified {
				// Archived originals must survive log retention; the cross-protocol
				// handler also owns a separate in-memory response session store.
				_, err := client.RequestExecution.Delete().Exec(ctx)
				require.NoError(t, err)
				_, err = client.Request.Delete().Exec(ctx)
				require.NoError(t, err)
			}
			current, err := client.Channel.Query().Only(ctx)
			require.NoError(t, err)
			require.NoError(t, current.Update().SetPolicies(objects.ChannelPolicies{RemoteCompaction: policy}).
				SetEndpoints([]objects.ChannelEndpoint{{APIFormat: "openai/responses"}}).
				SetSettings(&objects.ChannelSettings{PassThroughBody: lo.ToPtr(true), ModelProtocols: []objects.ModelProtocol{{Model: "gpt-6-astra", APIFormats: []string{"openai/responses"}}}}).Exec(ctx))
			require.NoError(t, handlers.ChannelService.ReloadEnabledChannelsCache(ctx))
			continuationBody := `{"model":"gpt-6-astra","stream":false,"client_metadata":{"thread_id":"delta-thread"},"input":[{"role":"user","content":"retained detail"},{"type":"compaction","id":"cmp_ws_source","encrypted_content":"source-cipher"},{"role":"user","content":"continue-canary"}]}`
			if unified {
				continuationBody = strings.ReplaceAll(continuationBody, "retained detail", "complete original source canary")
			}
			for range 2 {
				status, body := responsesIntegrationPost(t, gateway, "/v1/responses", continuationBody)
				require.Equal(t, http.StatusOK, status, string(body))
			}
			mu.Lock()
			defer mu.Unlock()
			generations := 0
			for _, body := range migrated {
				require.Empty(t, gjson.GetBytes(body, "previous_response_id").String())
				if gjson.GetBytes(body, `input.#(type=="compaction_trigger")`).Exists() || gjson.GetBytes(body, "tool_choice").String() == "none" {
					generations++
					require.Contains(t, string(body), "complete original source canary")
					require.Contains(t, string(body), "original exact arguments")
					require.Contains(t, string(body), "original exact tool result")
				}
			}
			if unified {
				require.Zero(t, generations, "portable original history must not call compact or summarize")
				successes := 0
				for _, body := range migrated {
					if gjson.GetBytes(body, `input.#(id=="cmp_ws_source")`).Exists() {
						continue
					}
					successes++
					require.Contains(t, string(body), "complete original source canary")
					require.Contains(t, string(body), "original exact arguments")
					require.Contains(t, string(body), "original exact tool result")
					require.Contains(t, string(body), "continue-canary")
				}
				require.Equal(t, 2, successes)
			} else {
				require.Equal(t, 1, generations, "the next continuation must reuse the migrated checkpoint or local summary")
			}
		})
	}
}
