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

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
)

func TestResponsesProtocolAutoCompactionNativeFirst(t *testing.T) {
	for _, transport := range []string{"http", "ws"} {
		for _, passThrough := range []bool{true, false} {
			for _, trigger := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/pass-through=%t/trigger=%t", transport, passThrough, trigger), func(t *testing.T) {
					var mu sync.Mutex
					var bodies [][]byte
					upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						body, err := readRequestBody(r)
						if err != nil {
							t.Error(err)
							return
						}
						mu.Lock()
						bodies = append(bodies, body)
						mu.Unlock()
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_native_first\",\"object\":\"response\",\"model\":\"gpt-6-astra\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"continued\"}]}]}}\n\n")
					}))
					client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL, func(create *ent.ChannelCreate) {
						create.SetType(channel.TypeCodex).SetSupportedModels([]string{"synthetic-model", "gpt-6-astra"}).
							SetPolicies(objects.ChannelPolicies{RemoteCompaction: objects.RemoteCompactionPolicyAuto}).
							SetSettings(&objects.ChannelSettings{PassThroughBody: lo.ToPtr(passThrough)})
					})
					gateway := responsesIntegrationGateway(t, client, handlers, project)
					payload := map[string]any{
						"model": "gpt-6-astra", "stream": false, "prompt_cache_key": "stable-native-key",
						"reasoning":       map[string]string{"effort": "xhigh"},
						"client_metadata": map[string]string{"thread_id": "native-first-thread"},
						"input":           []any{map[string]string{"role": "user", "content": "retained-user"}, map[string]string{"type": "compaction", "id": "cmp_native_first", "encrypted_content": "opaque-native-prefix"}},
					}
					if trigger {
						payload["input"] = append(payload["input"].([]any), map[string]string{"type": "compaction_trigger"})
					}
					if transport == "http" {
						encoded, err := json.Marshal(payload)
						require.NoError(t, err)
						status, result := responsesIntegrationPost(t, gateway, "/v1/responses", string(encoded))
						require.Equal(t, http.StatusOK, status, string(result))
					} else {
						delete(payload, "stream")
						payload["type"] = "response.create"
						encoded, err := json.Marshal(payload)
						require.NoError(t, err)
						conn := responsesIntegrationDial(t, gateway)
						result := responsesIntegrationTurn(t, conn, string(encoded))
						require.Equal(t, "response.completed", gjson.GetBytes(result, "type").String(), string(result))
					}
					mu.Lock()
					defer mu.Unlock()
					require.Len(t, bodies, 1, "native acceptance must not need a summary or source-history lookup")
					require.Equal(t, "opaque-native-prefix", gjson.GetBytes(bodies[0], `input.#(type=="compaction").encrypted_content`).String())
					require.Equal(t, "stable-native-key", gjson.GetBytes(bodies[0], "prompt_cache_key").String())
					require.Equal(t, "xhigh", gjson.GetBytes(bodies[0], "reasoning.effort").String())
					require.Equal(t, trigger, gjson.GetBytes(bodies[0], `input.#(type=="compaction_trigger")`).Exists())
					require.NotContains(t, string(bodies[0]), "CONTEXT CHECKPOINT COMPACTION")
				})
			}
		}
	}
}
