package api

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
)

// Model the iceberg failure through real client handlers, source retention,
// retries, persistence and subsequent previous_response_id materialization.
func TestResponsesProtocolRedactedEncryptionContinuation(t *testing.T) {
	testResponsesProtocolEncryptionContinuation(t, "The encrypted content gAAA...AA== could not be verified. Reason: Encrypted content could not be decrypted or parsed.", "", "", false)
}

func TestResponsesProtocolInvalidEncryptedSummaryContinuation(t *testing.T) {
	testResponsesProtocolEncryptionContinuation(t, "invalid encrypted summary (request id: synthetic_59958)", "validation_error", "", false)
}

func TestResponsesProtocolReasoningHydrationContinuation(t *testing.T) {
	testResponsesProtocolEncryptionContinuation(t, "invalid encrypted summary", "validation_error", "The encrypted content for item rs_old could not be verified. Reason: reasoning hydration failed: Encrypted content could not be decrypted or parsed.", false)
}

// Exercise the production middleware order, including encrypted tool arguments
// returned by the portable agent transport, not just a manually ordered pipeline.
func TestResponsesProtocolEncryptedAgentCompactionContinuation(t *testing.T) {
	testResponsesProtocolEncryptionContinuation(t, "invalid encrypted summary", "validation_error", "", true)
}

func responsesProtocolSealedAgentMessage(t *testing.T, projectID, apiKeyID int) string {
	t.Helper()
	key := hmac.New(sha256.New, []byte("synthetic-iceberg-secret"))
	_, err := key.Write([]byte("axonhub/agent-message/v1"))
	require.NoError(t, err)
	block, err := aes.NewCipher(key.Sum(nil))
	require.NoError(t, err)
	aead, err := cipher.NewGCM(block)
	require.NoError(t, err)
	// A fixed nonce is only used for this synthetic, fixed-plaintext fixture.
	nonce := make([]byte, aead.NonceSize())
	aad := fmt.Appendf(nil, "axonhub/agent-message/v1:%d:%d:", projectID, apiKeyID)
	sealed := aead.Seal(nonce, nonce, []byte("exact-agent-instructions-canary"), aad)
	return "axonhub-agent-v1." + base64.RawURLEncoding.EncodeToString(sealed)
}

func testResponsesProtocolEncryptionContinuation(t *testing.T, rejectionMessage, rejectionCode, reasoningMessage string, encryptedAgents bool) {
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
					if encryptedAgents {
						sealed := responsesProtocolSealedAgentMessage(t, project.ID, 101)
						arguments, err := json.Marshal(map[string]string{"task_name": "worker", "message": sealed})
						require.NoError(t, err)
						for _, item := range []any{
							map[string]any{"type": "function_call", "id": "fc_agent", "call_id": "call_agent", "name": "spawn_agent", "namespace": "agents", "arguments": string(arguments), "encrypted_function_args": []string{"message"}},
							map[string]string{"type": "function_call_output", "call_id": "call_agent", "output": "worker-started-canary"},
							map[string]any{"type": "agent_message", "id": "am_worker", "author": "/root/worker", "recipient": "/root", "content": []any{
								map[string]string{"type": "input_text", "text": "agent-delivery-prefix-canary"},
								map[string]string{"type": "encrypted_content", "encrypted_content": sealed},
							}},
						} {
							continuation, err = sjson.Set(continuation, "input.-1", item)
							require.NoError(t, err)
						}
					}
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
					wantAttempts := 6
					if encryptedAgents {
						// A fresh client resends the sealed history. Confirmed checkpoint
						// recovery must still decrypt it before checking completeness.
						status, response := responsesIntegrationPost(t, gateway, "/v1/responses", continuation)
						require.Equal(t, http.StatusOK, status, string(response))
						wantAttempts++
						for _, badMessage := range []string{
							responsesProtocolSealedAgentMessage(t, project.ID, 102),
							"axonhub-agent-v1.invalid",
							"unresolved-foreign-ciphertext",
						} {
							arguments, err := json.Marshal(map[string]string{"message": badMessage})
							require.NoError(t, err)
							invalid, err := sjson.Set(continuation, "input.6.arguments", string(arguments))
							require.NoError(t, err)
							status, _ := responsesIntegrationPost(t, gateway, "/v1/responses", invalid)
							require.NotEqual(t, http.StatusOK, status, "unverifiable arguments must stop before upstream execution")
						}
					}
					mu.Lock()
					defer mu.Unlock()
					require.Equal(t, 2, rejections)
					require.Equal(t, 1, summaries)
					require.Len(t, attempts, wantAttempts)
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
					if encryptedAgents {
						for index, body := range attempts {
							if index == 0 || index == 3 {
								continue // Original source and its independent summary request.
							}
							call := gjson.GetBytes(body, `input.#(call_id=="call_agent")`)
							require.Equal(t, "exact-agent-instructions-canary", gjson.Get(call.Get("arguments").String(), "message").String())
							require.False(t, call.Get("encrypted_function_args").Exists())
							require.Equal(t, "exact-agent-instructions-canary", gjson.GetBytes(body, `input.#(id=="am_worker").content.1.text`).String())
							require.Contains(t, string(body), "agent-delivery-prefix-canary")
							require.Contains(t, string(body), "worker-started-canary")
							require.NotContains(t, string(body), "axonhub-agent-v1.")
						}
					}
					require.Contains(t, string(attempts[5]), "next-turn-canary")
				})
			}
		}
	}
}
