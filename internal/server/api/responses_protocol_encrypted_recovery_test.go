package api

import (
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

// Model the iceberg failure through real client handlers, source retention,
// retries, persistence and subsequent previous_response_id materialization.
func TestResponsesProtocolRedactedEncryptionContinuation(t *testing.T) {
	testResponsesProtocolEncryptionContinuation(t, "The encrypted content gAAA...AA== could not be verified. Reason: Encrypted content could not be decrypted or parsed.", "", "")
}

func TestResponsesProtocolInvalidEncryptedSummaryContinuation(t *testing.T) {
	testResponsesProtocolEncryptionContinuation(t, "invalid encrypted summary (request id: synthetic_59958)", "validation_error", "")
}

func TestResponsesProtocolReasoningHydrationContinuation(t *testing.T) {
	testResponsesProtocolEncryptionContinuation(t, "invalid encrypted summary", "validation_error", "The encrypted content for item rs_old could not be verified. Reason: reasoning hydration failed: Encrypted content could not be decrypted or parsed.")
}

func testResponsesProtocolEncryptionContinuation(t *testing.T, rejectionMessage, rejectionCode, reasoningMessage string) {
	for _, model := range []string{"gpt-6-astra", "gpt-6.1-sol"} {
		for _, downstream := range []string{"http", "ws"} {
			for _, passThrough := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/raw=%t", model, downstream, passThrough), func(t *testing.T) {
					var mu sync.Mutex
					var attempts [][]byte
					var summaries, rejections int
					upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						body, err := readRequestBody(r)
						if err != nil {
							t.Error(err)
							return
						}
						mu.Lock()
						defer mu.Unlock()
						attempts = append(attempts, body)
						if gjson.GetBytes(body, `input.#(type=="compaction")`).Exists() || gjson.GetBytes(body, `input.#(type=="reasoning").encrypted_content`).String() != "" {
							rejections++
							message, code, param := rejectionMessage, rejectionCode, ""
							if reasoningMessage != "" && gjson.GetBytes(body, `input.#(type=="reasoning").encrypted_content`).String() != "" {
								message, code, param = reasoningMessage, "invalid_encrypted_content", "input"
							}
							w.WriteHeader(http.StatusBadRequest)
							_, _ = fmt.Fprintf(w, `{"error":{"type":"invalid_request_error","code":%q,"param":%q,"message":%q}}`, code, param, message)
							return
						}
						output := `[{"type":"message","id":"msg_iceberg","role":"assistant","content":[{"type":"output_text","text":"recovered"}]}]`
						responseID := "resp_iceberg"
						if gjson.GetBytes(body, `input.#(type=="compaction_trigger")`).Exists() {
							output = `[{"type":"compaction","id":"cmp_iceberg","encrypted_content":"checkpoint-cipher"}]`
							responseID = "resp_source"
						} else if gjson.GetBytes(body, "tool_choice").String() == "none" {
							summaries++
							output = `[{"type":"message","id":"msg_summary","role":"assistant","content":[{"type":"output_text","text":"retained-source-summary-canary"}]}]`
							responseID = "resp_summary"
						}
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":%q,\"object\":\"response\",\"model\":%q,\"status\":\"completed\",\"output\":%s,\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}}\n\n", responseID, model, output)
					}))
					client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL, func(create *ent.ChannelCreate) {
						create.SetType(channel.TypeCodex).SetSupportedModels([]string{"synthetic-model", model}).
							SetSettings(&objects.ChannelSettings{PassThroughBody: lo.ToPtr(passThrough)}).
							SetPolicies(objects.ChannelPolicies{RemoteCompaction: objects.RemoteCompactionPolicyNative})
					})
					ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
					require.NoError(t, handlers.SystemService.SetSecretKey(ctx, "synthetic-iceberg-secret"))
					policy := handlers.SystemService.RetryPolicyOrDefault(ctx)
					policy.Enabled, policy.MaxChannelRetries, policy.MaxSingleChannelRetries, policy.RetryDelayMs = true, 0, 2, 0
					require.NoError(t, handlers.SystemService.SetRetryPolicy(ctx, policy))
					gateway := responsesIntegrationGateway(t, client, handlers, project)
					root := fmt.Sprintf(`{"model":%q,"stream":false,"client_metadata":{"thread_id":"iceberg-thread","x-codex-window-id":"iceberg-window"},"input":[{"role":"user","content":"complete-source-canary"},{"type":"compaction_trigger"}]}`, model)
					status, checkpoint := responsesIntegrationPost(t, gateway, "/v1/responses", root)
					require.Equal(t, http.StatusOK, status, string(checkpoint))
					require.Equal(t, "cmp_iceberg", gjson.GetBytes(checkpoint, "output.0.id").String())
					continuation := fmt.Sprintf(`{"model":%q,"stream":false,"client_metadata":{"thread_id":"iceberg-thread","x-codex-window-id":"iceberg-window"},"input":[{"type":"message","role":"user","content":"retained-user-canary"},{"type":"compaction","id":"cmp_iceberg","encrypted_content":"checkpoint-cipher"},{"type":"reasoning","id":"rs_old","encrypted_content":"gAAAAABsynthetic_old_reasoning_ciphertext_0123456789abcdef0123456789abcdef","summary":[{"type":"summary_text","text":"visible-reasoning-canary"}]},{"type":"custom_tool_call","id":"ct_old","call_id":"call_retained","name":"inspect","input":"tool-input-canary"},{"type":"custom_tool_call_output","call_id":"call_retained","output":"tool-output-canary"},{"role":"user","content":"continue-canary"}]}`, model)
					if downstream == "http" {
						status, response := responsesIntegrationPost(t, gateway, "/v1/responses", continuation)
						require.Equal(t, http.StatusOK, status, string(response))
						status, response = responsesIntegrationPost(t, gateway, "/v1/responses", fmt.Sprintf(`{"model":%q,"previous_response_id":"resp_iceberg","input":"next-turn-canary"}`, model))
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
						require.Equal(t, "resp_iceberg", gjson.GetBytes(response, "response.id").String(), string(response))
						response = responsesIntegrationTurn(t, conn, fmt.Sprintf(`{"type":"response.create","model":%q,"previous_response_id":"resp_iceberg","input":"next-turn-canary"}`, model))
						require.Equal(t, "response.completed", gjson.GetBytes(response, "type").String(), string(response))
					}
					mu.Lock()
					defer mu.Unlock()
					require.Equal(t, 2, rejections)
					require.Equal(t, 1, summaries)
					require.Len(t, attempts, 6)
					require.Contains(t, string(attempts[1]), "gAAAAABsynthetic_old_reasoning_ciphertext_0123456789abcdef0123456789abcdef")
					require.Contains(t, string(attempts[2]), "checkpoint-cipher")
					require.NotContains(t, string(attempts[2]), "gAAAAABsynthetic_old_reasoning_ciphertext_0123456789abcdef0123456789abcdef")
					require.Contains(t, string(attempts[3]), "complete-source-canary")
					for _, body := range attempts[4:] {
						for _, text := range []string{"retained-source-summary-canary", "retained-user-canary", "visible-reasoning-canary", "tool-input-canary", "tool-output-canary", "call_retained", "continue-canary"} {
							require.Contains(t, string(body), text)
						}
						require.NotContains(t, string(body), "checkpoint-cipher")
						require.NotContains(t, string(body), "gAAAAABsynthetic_old_reasoning_ciphertext_0123456789abcdef0123456789abcdef")
						require.False(t, gjson.GetBytes(body, "previous_response_id").Exists())
					}
					require.Contains(t, string(attempts[5]), "next-turn-canary")
				})
			}
		}
	}
}
