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
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
)

func TestResponsesProtocolSessionLifecycle(t *testing.T) {
	for _, downstream := range []string{"http", "ws"} {
		for _, checkpoint := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/checkpoint=%t", downstream, checkpoint), func(t *testing.T) {
				var mu sync.Mutex
				var bodies [][]byte
				var sessions []string
				var rejectNext bool
				upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, err := readRequestBody(r)
					require.NoError(t, err)
					mu.Lock()
					defer mu.Unlock()
					bodies = append(bodies, body)
					sessions = append(sessions, r.Header.Get("Session-Id"))
					if rejectNext {
						rejectNext = false
						w.WriteHeader(http.StatusConflict)
						_, _ = fmt.Fprint(w, `{"error":{"code":"session_binding_invalidated","message":"The conversation binding has expired."}}`)
						return
					}
					output := `[{"type":"message","id":"msg_reply","role":"assistant","content":[{"type":"output_text","text":"continued"}]}]`
					if gjson.GetBytes(body, `input.#(type=="compaction_trigger")`).Exists() {
						output = `[{"type":"compaction","id":"cmp_binding","encrypted_content":"retained-native-checkpoint"}]`
					}
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_binding_%d\",\"object\":\"response\",\"status\":\"completed\",\"model\":%q,\"output\":%s,\"usage\":{\"input_tokens\":100,\"output_tokens\":2,\"total_tokens\":102}}}\n\n", len(bodies), gjson.GetBytes(body, "model").String(), output)
				}))
				client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL, func(create *ent.ChannelCreate) {
					create.SetType(channel.TypeCodex).SetSupportedModels([]string{"synthetic-model", "gpt-6-astra", "gpt-6.1-sol"}).SetSettings(&objects.ChannelSettings{PassThroughBody: lo.ToPtr(true)}).SetPolicies(objects.ChannelPolicies{RemoteCompaction: objects.RemoteCompactionPolicyNative})
				})
				ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
				require.NoError(t, handlers.SystemService.SetSecretKey(ctx, "synthetic-binding-secret"))
				policy := handlers.SystemService.RetryPolicyOrDefault(ctx)
				policy.Enabled, policy.MaxChannelRetries, policy.MaxSingleChannelRetries, policy.RetryDelayMs = true, 0, 2, 0
				require.NoError(t, handlers.SystemService.SetRetryPolicy(ctx, policy))
				gateway := responsesIntegrationGateway(t, client, handlers, project)
				metadata := map[string]string{"session_id": "local-session", "thread_id": "local-thread", "x-codex-window-id": "local-thread:4", "x-codex-turn-metadata": `{"session_id":"local-session","thread_id":"local-thread","window_id":"local-thread:4","request_kind":"turn"}`}
				root, err := sjson.Set(`{"model":"gpt-6-astra","stream":false,"prompt_cache_key":"local-thread","input":[{"role":"user","content":"retained-source-canary"}]}`, "client_metadata", metadata)
				require.NoError(t, err)
				seed := root
				if checkpoint {
					seed, err = sjson.SetRaw(seed, "input.-1", `{"type":"compaction_trigger"}`)
					require.NoError(t, err)
				}
				status, response := responsesIntegrationPost(t, gateway, "/v1/responses", seed)
				require.Equal(t, http.StatusOK, status, string(response))
				full := root
				if checkpoint {
					full, err = sjson.SetRaw(full, "input.-1", `{"type":"compaction","id":"cmp_binding","encrypted_content":"retained-native-checkpoint"}`)
					require.NoError(t, err)
				}
				for _, item := range []string{`{"type":"reasoning","id":"rs_native","encrypted_content":"native-reasoning-canary","summary":[]}`, `{"type":"function_call","id":"fc_history","call_id":"call_history","name":"inspect","arguments":"{\"n\":9007199254740993}"}`, `{"type":"function_call_output","call_id":"call_history","output":"exact-tool-result"}`, `{"role":"user","content":"continue-original-thread"}`} {
					full, err = sjson.SetRaw(full, "input.-1", item)
					require.NoError(t, err)
				}
				mu.Lock()
				rejectNext = true
				mu.Unlock()
				submit := func(body string) []byte {
					status, response := responsesIntegrationPost(t, gateway, "/v1/responses", body)
					require.Equal(t, http.StatusOK, status, string(response))
					return response
				}
				if downstream == "ws" {
					conn := responsesIntegrationDial(t, gateway)
					submit = func(body string) []byte {
						payload := make(map[string]json.RawMessage)
						require.NoError(t, json.Unmarshal([]byte(body), &payload))
						delete(payload, "stream")
						payload["type"] = json.RawMessage(`"response.create"`)
						encoded, err := json.Marshal(payload)
						require.NoError(t, err)
						event := responsesIntegrationTurn(t, conn, string(encoded))
						require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String(), string(event))
						return []byte(gjson.GetBytes(event, "response").Raw)
					}
				}
				response = submit(full)
				require.Equal(t, "resp_binding_3", gjson.GetBytes(response, "id").String())
				delta, err := sjson.Set(root, "previous_response_id", "resp_binding_3")
				require.NoError(t, err)
				delta, err = sjson.SetRaw(delta, "input", `[{"role":"user","content":"delta-canary"}]`)
				require.NoError(t, err)
				submit(delta)
				// A model switch selects a distinct binding; switching back reuses
				// the repaired one. No provider-specific error is needed for either.
				otherModel, err := sjson.Set(full, "model", "gpt-6.1-sol")
				require.NoError(t, err)
				submit(otherModel)
				submit(full)
				mu.Lock()
				defer mu.Unlock()
				require.Len(t, bodies, 6)
				require.NotEqual(t, "local-session", sessions[0])
				require.Equal(t, sessions[0], sessions[1])
				require.NotEqual(t, sessions[1], sessions[2])
				require.Equal(t, sessions[2], sessions[3])
				require.NotEqual(t, sessions[3], sessions[4])
				require.Equal(t, sessions[2], sessions[5])
				require.Equal(t, gjson.GetBytes(bodies[1], "input").Raw, gjson.GetBytes(bodies[2], "input").Raw)
				require.NotContains(t, string(bodies[3]), "previous_response_id")
				for _, canary := range []string{"retained-source-canary", "native-reasoning-canary", "9007199254740993", "exact-tool-result", "delta-canary"} {
					require.Contains(t, string(bodies[3]), canary)
				}
				if checkpoint {
					require.Contains(t, string(bodies[3]), "retained-native-checkpoint")
				}
				for _, body := range bodies[2:] {
					require.Equal(t, gjson.GetBytes(body, "prompt_cache_key").String(), gjson.GetBytes(body, "client_metadata.thread_id").String())
				}
			})
		}
	}
}
