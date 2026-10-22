package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

// Exercise the real handlers and middleware ordering: source retention, local
// expansion, retries, completed snapshot capture, and the following delta turn.
func TestResponsesProtocolCompactionRelayContinuation(t *testing.T) {
	for _, model := range []string{"gpt-6-astra", "gpt-6.1-sol"} {
		for _, downstream := range []string{"http", "ws"} {
			for _, passThrough := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/pass-through=%t", model, downstream, passThrough), func(t *testing.T) {
					var mu sync.Mutex
					var bodies [][]byte
					var methods []string
					var summaryCalls, continuationCalls int
					upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						body, err := readRequestBody(r)
						if err != nil {
							t.Error(err)
							return
						}
						mu.Lock()
						defer mu.Unlock()
						bodies = append(bodies, body)
						methods = append(methods, r.Method)
						if r.Method != http.MethodPost {
							w.WriteHeader(http.StatusNotFound)
							_, _ = io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"Invalid URL (GET /v1/responses)"}}`)
							return
						}
						if gjson.GetBytes(body, `input.#(type=="compaction")`).Exists() {
							w.WriteHeader(http.StatusBadRequest)
							_, _ = io.WriteString(w, relayResourceMismatchFixture)
							return
						}
						output := `[{"type":"message","id":"msg_relay","role":"assistant","content":[{"type":"output_text","text":"relay-complete"}]}]`
						responseID := "resp_relay"
						switch {
						case gjson.GetBytes(body, `input.#(type=="compaction_trigger")`).Exists():
							output = `[{"type":"compaction","id":"cmp_relay","encrypted_content":"native-source-ciphertext"}]`
							responseID = "resp_checkpoint"
						case bytes.Contains(body, []byte("You are performing a CONTEXT CHECKPOINT COMPACTION.")):
							summaryCalls++
							output = `[{"type":"message","id":"msg_summary","role":"assistant","content":[{"type":"output_text","text":"verified-summary-canary"}]}]`
							responseID = "resp_summary"
						default:
							continuationCalls++
							if gjson.GetBytes(body, `input.#(type=="reasoning").encrypted_content`).String() != "" {
								w.WriteHeader(http.StatusBadRequest)
								_, _ = io.WriteString(w, relayResourceMismatchFixture)
								return
							}
						}
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":%q,\"object\":\"response\",\"model\":%q,\"status\":\"completed\",\"output\":%s,\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}}\n\n", responseID, model, output)
					}))
					client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL, func(create *ent.ChannelCreate) {
						create.SetType(channel.TypeCodex).SetSupportedModels([]string{"synthetic-model", model}).
							SetSettings(&objects.ChannelSettings{PassThroughBody: lo.ToPtr(passThrough)}).
							SetPolicies(objects.ChannelPolicies{RemoteCompaction: objects.RemoteCompactionPolicyAuto})
					})
					ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
					require.NoError(t, handlers.SystemService.SetSecretKey(ctx, "synthetic-compaction-relay-secret"))
					policy := handlers.SystemService.RetryPolicyOrDefault(ctx)
					policy.Enabled, policy.MaxChannelRetries, policy.MaxSingleChannelRetries, policy.RetryDelayMs = true, 0, 3, 0
					require.NoError(t, handlers.SystemService.SetRetryPolicy(ctx, policy))
					gateway := responsesIntegrationGateway(t, client, handlers, project)
					root := fmt.Sprintf(`{"model":%q,"stream":false,"client_metadata":{"thread_id":"relay-thread","x-codex-window-id":"relay-window"},"input":[{"role":"user","content":"full-source-canary"},{"type":"compaction_trigger"}]}`, model)
					status, checkpoint := responsesIntegrationPost(t, gateway, "/v1/responses", root)
					require.Equal(t, http.StatusOK, status, string(checkpoint))
					require.Equal(t, "cmp_relay", gjson.GetBytes(checkpoint, "output.0.id").String(), string(checkpoint))
					continuation := fmt.Sprintf(`{"model":%q,"stream":false,"client_metadata":{"thread_id":"relay-thread","x-codex-window-id":"relay-window"},"input":[{"type":"message","id":"msg_before","role":"user","content":"retained-user-canary"},{"type":"compaction","id":"cmp_relay","encrypted_content":"native-source-ciphertext"},{"type":"reasoning","id":"rs_old","encrypted_content":"gAAAAABsynthetic_old_reasoning_ciphertext_0123456789abcdef0123456789abcdef","summary":[{"type":"summary_text","text":"visible-reasoning-canary"}]},{"type":"custom_tool_call","id":"ct_old","call_id":"call_retained","name":"inspect","input":"tool-input-canary"},{"type":"custom_tool_call_output","id":"cto_old","call_id":"call_retained","output":"tool-output-canary"},{"type":"message","id":"msg_continue","role":"user","content":"continue-canary"}]}`, model)
					if downstream == "http" {
						status, response := responsesIntegrationPost(t, gateway, "/v1/responses", continuation)
						require.Equal(t, http.StatusOK, status, string(response))
						require.Equal(t, "resp_relay", gjson.GetBytes(response, "id").String(), string(response))
						status, response = responsesIntegrationPost(t, gateway, "/v1/responses", fmt.Sprintf(`{"model":%q,"previous_response_id":"resp_relay","input":"next-turn-canary"}`, model))
						require.Equal(t, http.StatusOK, status, string(response))
					} else {
						conn := responsesIntegrationDial(t, gateway)
						var payload map[string]any
						require.NoError(t, json.Unmarshal([]byte(continuation), &payload))
						delete(payload, "stream")
						payload["type"] = "response.create"
						encoded, err := json.Marshal(payload)
						require.NoError(t, err)
						response := responsesIntegrationTurn(t, conn, string(encoded))
						require.Equal(t, "resp_relay", gjson.GetBytes(response, "response.id").String(), string(response))
						response = responsesIntegrationTurn(t, conn, fmt.Sprintf(`{"type":"response.create","model":%q,"previous_response_id":"resp_relay","input":"next-turn-canary"}`, model))
						require.Equal(t, "response.completed", gjson.GetBytes(response, "type").String(), string(response))
					}
					mu.Lock()
					defer mu.Unlock()
					require.Equal(t, 1, summaryCalls, "one migration summary is reused by delta continuation")
					require.Contains(t, string(bodies[2]), "full-source-canary")
					require.Equal(t, 3, continuationCalls, "one distinct reasoning rejection, then portable and delta continuation")
					require.Len(t, bodies, 6)
					require.False(t, gjson.GetBytes(bodies[3], `input.#(type=="compaction")`).Exists(), "the rejected native checkpoint is never resubmitted")
					require.NotEmpty(t, gjson.GetBytes(bodies[3], `input.#(type=="reasoning").encrypted_content`).String())
					for _, method := range methods {
						require.Equal(t, http.MethodPost, method, "an HTTP relay must not receive a WS handshake")
					}
					for _, body := range bodies[4:] {
						require.Contains(t, string(body), "verified-summary-canary")
						require.NotContains(t, string(body), "full-source-canary")
						require.Contains(t, string(body), "retained-user-canary")
						require.Contains(t, string(body), "tool-input-canary")
						require.Contains(t, string(body), "tool-output-canary")
						require.Contains(t, string(body), "call_retained")
						require.NotContains(t, string(body), "native-source-ciphertext")
						require.False(t, gjson.GetBytes(body, "previous_response_id").Exists())
					}
					require.NotContains(t, string(bodies[4]), "gAAAAABsynthetic_old_reasoning_ciphertext_0123456789abcdef0123456789abcdef")
					require.Contains(t, string(bodies[4]), "visible-reasoning-canary")
					require.Contains(t, string(bodies[5]), "next-turn-canary")
				})
			}
		}
	}
}

const relayResourceMismatchFixture = `{"error":{"type":"invalid_request_error","message":"The requested item was created under a different Azure OpenAI resource. Use the same resource that created the item to access it."}}`
