package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
)

func TestCheckpointProtocolSummaryStreamContinuation(t *testing.T) {
	for _, model := range []string{"gpt-6.2-astra", "gpt-6.2-sol"} {
		for _, downstream := range []string{"http", "ws"} {
			for _, raw := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/raw=%t", model, downstream, raw), func(t *testing.T) {
					var mu sync.Mutex
					var attempts [][]byte
					var sourceCalls, summaryCalls int
					upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						body, err := readRequestBody(r)
						if err != nil {
							t.Error(err)
							return
						}
						mu.Lock()
						defer mu.Unlock()
						attempts = append(attempts, body)
						trigger := gjson.GetBytes(body, `input.#(type=="compaction_trigger")`).Exists()
						summary := bytes.Contains(body, []byte("You are performing a CONTEXT CHECKPOINT COMPACTION."))
						cipher := gjson.GetBytes(body, `input.#(type=="compaction")`).Exists() || gjson.GetBytes(body, `input.#(type=="reasoning").encrypted_content`).String() != ""
						if summary {
							summaryCalls++
							if gjson.GetBytes(body, "input.0.internal_chat_message_metadata_passthrough").Exists() {
								w.WriteHeader(400)
								_, _ = fmt.Fprint(w, `{"error":{"code":"invalid_request_unclassified","message":"Unknown parameter: 'input[0].internal_chat_message_metadata_passthrough.content_token_count'. [trace_id=synthetic code=30000]"}}`)
								return
								}
						}
						if !trigger && cipher {
							w.Header().Set("Content-Type", "text/event-stream")
							_, _ = fmt.Fprint(w, "data: {\"type\":\"response.created\",\"response\":{\"status\":\"in_progress\",\"output\":[]}}\n\n")
							_, _ = fmt.Fprint(w, "data: {\"type\":\"error\",\"status\":400,\"error\":{\"type\":\"invalid_request_error\",\"code\":\"invalid_request_unclassified\",\"message\":\"The encrypted content gAAA...XyW3 could not be verified. Reason: Encrypted content could not be decrypted or parsed. [trace_id=synthetic code=30000]\"}}\n\n")
							return
						}
						id, output := "resp_recovered", `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"continued"}]}]`
						if trigger {
							sourceCalls++
							id = fmt.Sprintf("resp_source_%d", sourceCalls)
							output = fmt.Sprintf(`[{"type":"compaction","id":"cmp_source_%d","encrypted_content":"cipher_%d"}]`, sourceCalls, sourceCalls)
						} else if summary {
							id, output = "resp_summary", `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"durable-summary-canary"}]}]`
						}
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":%q,\"object\":\"response\",\"model\":%q,\"status\":\"completed\",\"output\":%s}}\n\n", id, model, output)
					}))
					client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL, func(create *ent.ChannelCreate) {
						create.SetType(channel.TypeCodex).SetSupportedModels([]string{"synthetic-model", model}).SetSettings(&objects.ChannelSettings{PassThroughBody: lo.ToPtr(raw)}).SetPolicies(objects.ChannelPolicies{RemoteCompaction: objects.RemoteCompactionPolicyNative})
					})
					ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
					require.NoError(t, handlers.SystemService.SetSecretKey(ctx, "synthetic-nested-summary-secret"))
					policy := handlers.SystemService.RetryPolicyOrDefault(ctx)
					policy.Enabled, policy.MaxChannelRetries, policy.MaxSingleChannelRetries, policy.RetryDelayMs = true, 0, 4, 0
						require.NoError(t, handlers.SystemService.SetRetryPolicy(ctx, policy))
					gateway := responsesIntegrationGateway(t, client, handlers, project)
					prefix := fmt.Sprintf(`{"model":%q,"stream":false,"prompt_cache_key":"stable-cache","client_metadata":{"thread_id":"nested-thread","x-codex-window-id":"nested-window"},"input":[`, model)
					user := `{"type":"message","role":"user","content":"retained-user-canary","internal_chat_message_metadata_passthrough":{"content_token_count":5}}`
					status, response := responsesIntegrationPost(t, gateway, "/v1/responses", prefix+user+`,{"role":"assistant","content":"ancestor-detail-canary"},{"type":"compaction_trigger"}]}`)
					require.Equal(t, 200, status, string(response))
					require.Equal(t, "cmp_source_1", gjson.GetBytes(response, "output.0.id").String())
					source := prefix + user + `,{"type":"compaction","id":"cmp_source_1","encrypted_content":"cipher_1"},{"type":"reasoning","id":"rs_source","encrypted_content":"source-reasoning","summary":[{"type":"summary_text","text":"source-reasoning-canary"}]},{"type":"custom_tool_call","call_id":"call_source","name":"inspect","input":"source-input-canary"},{"type":"custom_tool_call_output","call_id":"call_source","output":"source-output-canary"},{"type":"compaction_trigger"}]}`
					status, response = responsesIntegrationPost(t, gateway, "/v1/responses", source)
					require.Equal(t, 200, status, string(response))
					require.Equal(t, "cmp_source_2", gjson.GetBytes(response, "output.0.id").String())
					continuation := prefix + user + `,{"type":"compaction","id":"cmp_source_2","encrypted_content":"cipher_2"},{"type":"reasoning","id":"rs_latest","encrypted_content":"latest-reasoning","summary":[{"type":"summary_text","text":"latest-reasoning-canary"}]},{"role":"user","content":"continue-canary"}]}`
					if downstream == "http" {
						status, response = responsesIntegrationPost(t, gateway, "/v1/responses", continuation)
						require.Equal(t, 200, status, string(response))
					} else {
						conn := responsesIntegrationDial(t, gateway)
						var payload map[string]any
						require.NoError(t, json.Unmarshal([]byte(continuation), &payload))
						delete(payload, "stream")
						payload["type"] = "response.create"
						encoded, err := json.Marshal(payload)
						require.NoError(t, err)
						response = responsesIntegrationTurn(t, conn, string(encoded))
						require.Equal(t, "response.completed", gjson.GetBytes(response, "type").String(), string(response))
					}
					status, response = responsesIntegrationPost(t, gateway, "/v1/responses", fmt.Sprintf(`{"model":%q,"previous_response_id":"resp_recovered","input":"next-turn-canary"}`, model))
					require.Equal(t, 200, status, string(response))
					mu.Lock()
					defer mu.Unlock()
					require.Equal(t, 4, summaryCalls, "metadata, reasoning, ancestor rejection, then one successful summary")
					require.Len(t, attempts, 10)
					for _, value := range []string{"ancestor-detail-canary", "source-reasoning-canary", "source-input-canary", "source-output-canary"} {
						require.Contains(t, string(attempts[7]), value)
					}
					for _, body := range attempts[8:] {
						require.Contains(t, string(body), "durable-summary-canary")
						require.Contains(t, string(body), "latest-reasoning-canary")
						require.Contains(t, string(body), "continue-canary")
						require.NotContains(t, string(body), "cipher_2")
					}
					require.Contains(t, string(attempts[9]), "next-turn-canary")
				})
			}
		}
	}
}
