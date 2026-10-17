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

func TestResponsesProtocolNativeCompactionMigration(t *testing.T) {
	for _, transport := range []string{"http", "ws"} {
		for _, passThrough := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/raw=%t", transport, passThrough), func(t *testing.T) {
				var mu sync.Mutex
				var bodies [][]byte
				var compactions int
				upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := readRequestBody(r)
					if err != nil {
						t.Error(err)
						return
					}
					mu.Lock()
					defer mu.Unlock()
					bodies = append(bodies, body)
					if gjson.GetBytes(body, `input.#(id=="cmp_source")`).Exists() {
						w.WriteHeader(http.StatusBadRequest)
						_, _ = fmt.Fprint(w, `{"error":{"type":"invalid_request_error","message":"The encrypted content for item cmp_source could not be verified. Reason: Encrypted content could not be decrypted or parsed."}}`)
						return
					}
					output := `[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"continued with native state"}]}]`
					if gjson.GetBytes(body, `input.#(type=="compaction_trigger")`).Exists() {
						compactions++
						id := "cmp_source"
						if compactions > 1 {
							id = "cmp_destination"
						}
						output = fmt.Sprintf(`[{"type":"compaction","id":%q,"encrypted_content":%q}]`, id, id+"-cipher")
					}
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_migration\",\"object\":\"response\",\"model\":\"gpt-6-astra\",\"status\":\"completed\",\"output\":%s,\"usage\":{\"input_tokens\":20,\"output_tokens\":2,\"total_tokens\":22}}}\n\n", output)
				}))
				client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL, func(create *ent.ChannelCreate) {
					create.SetType(channel.TypeCodex).SetSupportedModels([]string{"synthetic-model", "gpt-6-astra"}).SetPolicies(objects.ChannelPolicies{RemoteCompaction: objects.RemoteCompactionPolicyAuto}).SetSettings(&objects.ChannelSettings{PassThroughBody: lo.ToPtr(passThrough)})
				})
				ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
				require.NoError(t, handlers.SystemService.SetSecretKey(ctx, "synthetic-native-migration-secret"))
				policy := handlers.SystemService.RetryPolicyOrDefault(ctx)
				policy.Enabled, policy.MaxChannelRetries, policy.MaxSingleChannelRetries, policy.RetryDelayMs = true, 0, 1, 0
				require.NoError(t, handlers.SystemService.SetRetryPolicy(ctx, policy))
				gateway := responsesIntegrationGateway(t, client, handlers, project)
				root := `{"model":"gpt-6-astra","stream":false,"prompt_cache_key":"stable-migration","client_metadata":{"thread_id":"migration-thread","x-codex-turn-metadata":"{\"request_kind\":\"compaction\",\"compaction\":{\"implementation\":\"responses_compaction_v2\"}}"},"input":[{"role":"user","content":"full unabridged source detail"},{"type":"compaction_trigger"}]}`
				status, result := responsesIntegrationPost(t, gateway, "/v1/responses", root)
				require.Equal(t, http.StatusOK, status, string(result))
				continuation := `{"model":"gpt-6-astra","stream":false,"prompt_cache_key":"stable-migration","reasoning":{"effort":"xhigh"},"client_metadata":{"thread_id":"migration-thread"},"input":[{"role":"user","content":"retained user detail"},{"type":"compaction","id":"cmp_source","encrypted_content":"cmp_source-cipher"},{"type":"function_call","name":"inspect","call_id":"call_exact","arguments":"{\"path\":\"exact\"}"},{"type":"function_call_output","call_id":"call_exact","output":"unabridged tool result"},{"role":"user","content":"continue"}]}`
				for range 2 {
					if transport == "http" {
						status, result = responsesIntegrationPost(t, gateway, "/v1/responses", continuation)
						require.Equal(t, http.StatusOK, status, string(result))
					} else {
						var payload map[string]any
						require.NoError(t, json.Unmarshal([]byte(continuation), &payload))
						delete(payload, "stream")
						payload["type"] = "response.create"
						encoded, err := json.Marshal(payload)
						require.NoError(t, err)
						result = responsesIntegrationTurn(t, responsesIntegrationDial(t, gateway), string(encoded))
						require.Equal(t, "response.completed", gjson.GetBytes(result, "type").String(), string(result))
					}
				}
				mu.Lock()
				defer mu.Unlock()
				require.Equal(t, 2, compactions, "original plus one native migration, never a text summary")
				require.Len(t, bodies, 5, "original, rejection, native migration, continuation and cached continuation")
				require.Contains(t, string(bodies[2]), "full unabridged source detail")
				for _, body := range bodies {
					require.NotContains(t, string(body), "CONTEXT CHECKPOINT COMPACTION")
				}
				for _, body := range bodies[3:] {
					require.Equal(t, "cmp_destination-cipher", gjson.GetBytes(body, `input.#(type=="compaction").encrypted_content`).String())
					require.Contains(t, string(body), "unabridged tool result")
					require.Contains(t, string(body), "call_exact")
					require.Equal(t, "xhigh", gjson.GetBytes(body, "reasoning.effort").String())
					require.Equal(t, "stable-migration", gjson.GetBytes(body, "prompt_cache_key").String())
				}
				require.JSONEq(t, gjson.GetBytes(bodies[3], "input").Raw, gjson.GetBytes(bodies[4], "input").Raw)
			})
		}
	}
}
