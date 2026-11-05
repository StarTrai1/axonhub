package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
)

func TestCodexMemoryRoutingHTTPAndWebSocket(t *testing.T) {
	for _, passThrough := range []bool{true, false} {
		for _, policy := range []objects.CodexMemoryPolicy{objects.CodexMemoryNormal, objects.CodexMemoryExclude, objects.CodexMemoryOnly} {
			t.Run(fmt.Sprintf("raw=%t/policy=%s", passThrough, policy), func(t *testing.T) {
				var calls atomic.Int32
				upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					n := calls.Add(1)
					body, err := readRequestBody(r)
					if err != nil {
						t.Error(err)
						return
					}
					if !strings.Contains(string(body), "unchanged-memory-canary") {
						t.Error("input changed")
					}
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_memory_policy_%d\",\"model\":\"synthetic-model\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"done\"}]}]}}\n\n", n)
				}))
				client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL, func(c *ent.ChannelCreate) {
					c.SetPolicies(objects.ChannelPolicies{CodexMemory: policy}).SetSettings(&objects.ChannelSettings{PassThroughBody: lo.ToPtr(passThrough)})
				})
				gateway := responsesIntegrationGateway(t, client, handlers, project)
				for _, kind := range []string{"memory", "consolidation", "turn"} {
					for _, ws := range []bool{false, true} {
						body := codexMemoryIntegrationBody(t, kind, "unchanged-memory-canary", true)
						if kind == "consolidation" {
							body, _ = sjson.Set(body, "client_metadata.x-codex-turn-metadata", `{"request_kind":"turn","thread_source":"memory_consolidation"}`)
						}
						body, _ = sjson.Set(body, "reasoning.effort", "medium")
						before := calls.Load()
						allowed := policy == objects.CodexMemoryNormal || (policy == objects.CodexMemoryOnly) == (kind != "turn")
						if ws {
							conn := responsesIntegrationDial(t, gateway)
							event := responsesIntegrationTurn(t, conn, body)
							if allowed {
								require.Equal(t, "response.completed", gjson.GetBytes(event, "type").String(), string(event))
							} else {
								require.NotEqual(t, "response.completed", gjson.GetBytes(event, "type").String(), string(event))
							}
						} else {
							status, result := responsesIntegrationPost(t, gateway, "/v1/responses", body)
							if allowed {
								require.Equal(t, http.StatusOK, status, string(result))
							} else {
								require.NotEqual(t, http.StatusOK, status, string(result))
							}
						}
						if allowed {
							require.Equal(t, before+1, calls.Load())
						} else {
							require.Equal(t, before, calls.Load(), "excluded channel must never be called")
						}
					}
				}
			})
		}
	}
}

func TestCodexMemoryRoutingRefusalAndTransientBudget(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(fmt.Sprint(blocked), func(t *testing.T) {
			var calls atomic.Int32
			upstream := responsesIntegrationServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if blocked {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = fmt.Fprint(w, `{"error":{"message":"content-blocked (request id: synthetic)"}}`)
				} else {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = fmt.Fprint(w, `{"error":{"message":"temporary backend unavailable"}}`)
				}
			}))
			client, handlers, project := newResponsesIntegrationFixture(t, upstream.URL, func(c *ent.ChannelCreate) {
				c.SetSettings(&objects.ChannelSettings{MaxSingleChannelRetries: lo.ToPtr(10), RetryableStatusCodes: []int{400, 500}})
			})
			ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
			policy := handlers.SystemService.RetryPolicyOrDefault(ctx)
			policy.Enabled = true
			policy.MaxSingleChannelRetries = 10
			policy.RetryDelayMs = 0
			require.NoError(t, handlers.SystemService.SetRetryPolicy(ctx, policy))
			gateway := responsesIntegrationGateway(t, client, handlers, project)
			status, body := responsesIntegrationPost(t, gateway, "/v1/responses", codexMemoryIntegrationBody(t, "memory", "memory-budget", false))
			if blocked {
				require.Equal(t, 400, status, string(body))
				require.Contains(t, string(body), "content-blocked")
				require.Equal(t, int32(1), calls.Load())
			} else {
				require.Equal(t, 500, status, string(body))
				require.Equal(t, int32(3), calls.Load())
			}
		})
	}
}
