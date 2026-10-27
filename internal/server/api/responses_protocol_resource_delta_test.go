package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
)

func TestResponsesProtocolCompactionRelayDeltaKeepsNativeCachePrefix(t *testing.T) {
	for _, downstream := range []string{"http", "ws"} {
		for _, rejection := range []string{"none", "checkpoint", "resource"} {
			t.Run(fmt.Sprintf("%s/second-rejection=%s", downstream, rejection), func(t *testing.T) {
				rejectCheckpoint := rejection != "none"
				var mu sync.Mutex
				var deltaBodies, summaryBodies [][]byte
				var summaryCalls, nextCalls int
				upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := readRequestBody(r)
					if err != nil {
						t.Error(err)
						return
					}
					mu.Lock()
					defer mu.Unlock()
					responseID := "resp_warm"
					output := `[{"type":"custom_tool_call","id":"ct_delta","call_id":"call_delta","name":"inspect","input":"next-command"}]`
					cached := 10000
					switch {
					case bytes.Contains(body, []byte("CONTEXT CHECKPOINT COMPACTION")):
						summaryCalls++
						summaryBodies = append(summaryBodies, body)
						responseID, cached = "resp_summary", 0
						output = `[{"type":"message","id":"msg_summary","role":"assistant","content":[{"type":"output_text","text":"verified-source-summary"}]}]`
					case gjson.GetBytes(body, `input.#(type=="compaction_trigger")`).Exists():
						responseID = "resp_checkpoint"
						output = `[{"type":"compaction","id":"cmp_native","encrypted_content":"native-checkpoint-ciphertext"}]`
					case bytes.Contains(body, []byte("next-delta-canary")):
						nextCalls++
						deltaBodies = append(deltaBodies, body)
						responseID = "resp_next"
						output = `[{"type":"message","id":"msg_next","role":"assistant","content":[{"type":"output_text","text":"continued"}]}]`
					case bytes.Contains(body, []byte("first-delta-canary")):
						deltaBodies = append(deltaBodies, body)
						if gjson.GetBytes(body, `input.#(type=="reasoning").id`).String() != "" && gjson.GetBytes(body, `input.#(type=="compaction")`).Exists() {
							w.WriteHeader(http.StatusBadRequest)
							_, _ = io.WriteString(w, relayResourceMismatchFixture)
							return
						}
						if rejectCheckpoint && gjson.GetBytes(body, `input.#(type=="compaction")`).Exists() {
							w.WriteHeader(http.StatusBadRequest)
							failure := relayResourceMismatchFixture
							if rejection == "checkpoint" {
								failure = `{"error":{"code":"invalid_encrypted_content","message":"The encrypted content for item cmp_native could not be verified. Reason: Encrypted content could not be decrypted or parsed."}}`
							}
							_, _ = io.WriteString(w, failure)
							return
						}
						responseID = "resp_delta"
						output = `[{"type":"reasoning","id":"rs_fresh","encrypted_content":"fresh-native-ciphertext","summary":[]},{"type":"custom_tool_call","id":"ct_next","call_id":"call_next","name":"inspect","input":"next-command"}]`
					}
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":%q,\"object\":\"response\",\"model\":\"gpt-6-astra\",\"status\":\"completed\",\"output\":%s,\"usage\":{\"input_tokens\":10100,\"input_tokens_details\":{\"cached_tokens\":%d},\"output_tokens\":10,\"total_tokens\":10110}}}\n\n", responseID, output, cached)
				}))
				client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL, func(create *ent.ChannelCreate) {
					create.SetType(channel.TypeCodex).SetSupportedModels([]string{"synthetic-model", "gpt-6-astra"}).
						SetPolicies(objects.ChannelPolicies{RemoteCompaction: objects.RemoteCompactionPolicyNative})
				})
				ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
				require.NoError(t, handlers.SystemService.SetSecretKey(ctx, "synthetic-resource-delta-secret"))
				policy := handlers.SystemService.RetryPolicyOrDefault(ctx)
				policy.Enabled, policy.MaxChannelRetries, policy.MaxSingleChannelRetries, policy.RetryDelayMs = true, 0, 3, 0
				require.NoError(t, handlers.SystemService.SetRetryPolicy(ctx, policy))
				gateway := responsesIntegrationGateway(t, client, handlers, project, 10*time.Second)
				metadata := `"client_metadata":{"thread_id":"native-cache-thread","x-codex-window-id":"native-cache-window"}`
				root := `{"model":"gpt-6-astra","stream":false,` + metadata + `,"input":[{"role":"user","content":"source-archive-canary"},{"type":"compaction_trigger"}]}`
				status, checkpoint := responsesIntegrationPost(t, gateway, "/v1/responses", root)
				require.Equal(t, http.StatusOK, status, string(checkpoint))
				warm := `{"model":"gpt-6-astra","stream":false,"store":false,"prompt_cache_key":"stable-cache",` + metadata + `,"input":[{"type":"message","id":"msg_user","role":"user","content":"active-task-canary"},{"type":"compaction","id":"cmp_native","encrypted_content":"native-checkpoint-ciphertext"},{"type":"reasoning","id":"rs_warm","encrypted_content":"gAAAAABnative_reasoning_ciphertext_0123456789abcdef0123456789abcdef","summary":[]},{"type":"custom_tool_call","id":"ct_warm","call_id":"call_warm","name":"inspect","input":"tool-input-canary"},{"type":"custom_tool_call_output","id":"cto_warm","call_id":"call_warm","output":"tool-output-canary"}]}`
				first := `{"model":"gpt-6-astra","stream":false,"store":false,"prompt_cache_key":"stable-cache",` + metadata + `,"previous_response_id":"resp_warm","input":[{"type":"custom_tool_call_output","call_id":"call_delta","output":"first-delta-canary"}]}`
				next := `{"model":"gpt-6-astra","stream":false,"store":false,"prompt_cache_key":"stable-cache",` + metadata + `,"previous_response_id":"resp_delta","input":[{"type":"custom_tool_call_output","call_id":"call_next","output":"next-delta-canary"}]}`
				if downstream == "http" {
					for _, body := range []string{warm, first, next} {
						status, response := responsesIntegrationPost(t, gateway, "/v1/responses", body)
						require.Equal(t, http.StatusOK, status, string(response))
						require.Equal(t, int64(10000), gjson.GetBytes(response, "usage.input_tokens_details.cached_tokens").Int())
					}
				} else {
					conn := responsesIntegrationDial(t, gateway)
					for _, body := range []string{warm, first, next} {
						var payload map[string]any
						require.NoError(t, json.Unmarshal([]byte(body), &payload))
						delete(payload, "stream")
						payload["type"] = "response.create"
						encoded, err := json.Marshal(payload)
						require.NoError(t, err)
						response := responsesIntegrationTurn(t, conn, string(encoded))
						require.Equal(t, "response.completed", gjson.GetBytes(response, "type").String(), string(response))
						require.Equal(t, int64(10000), gjson.GetBytes(response, "response.usage.input_tokens_details.cached_tokens").Int())
					}
				}
				mu.Lock()
				defer mu.Unlock()
				require.Equal(t, 1, summaryCalls, "one resource repair replaces the rejected checkpoint")
				require.Contains(t, string(summaryBodies[0]), "source-archive-canary")
				require.Equal(t, 1, nextCalls)
				require.Len(t, deltaBodies, 3)
				require.Contains(t, string(deltaBodies[0]), "native_reasoning_ciphertext", "initial native history is unchanged")
				require.Contains(t, string(deltaBodies[0]), "native-checkpoint-ciphertext")
				for _, body := range deltaBodies {
					require.Contains(t, string(body), "tool-output-canary")
					require.Contains(t, string(body), "first-delta-canary")
					require.Equal(t, "stable-cache", gjson.GetBytes(body, "prompt_cache_key").String())
					require.False(t, gjson.GetBytes(body, "previous_response_id").Exists())
				}
				for _, body := range deltaBodies[1:] {
					require.False(t, gjson.GetBytes(body, `input.#(type=="compaction")`).Exists())
					require.Contains(t, string(body), "verified-source-summary")
					require.NotContains(t, string(body), "source-archive-canary")
					require.NotContains(t, string(body), "native_reasoning_ciphertext")
				}
				last := deltaBodies[2]
				require.Equal(t, "rs_fresh", gjson.GetBytes(last, `input.#(type=="reasoning").id`).String(), "the next request keeps newly accepted reasoning")
				require.Equal(t, "fresh-native-ciphertext", gjson.GetBytes(last, `input.#(type=="reasoning").encrypted_content`).String())
				require.Equal(t, "ct_next", gjson.GetBytes(last, `input.#(call_id=="call_next").id`).String())
			})
		}
	}
}
