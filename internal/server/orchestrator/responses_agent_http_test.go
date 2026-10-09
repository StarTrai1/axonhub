package orchestrator

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/oauth"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/openai/codex"
)

func TestResponsesAgentTransportNonStreaming(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		baseURL string
	}{
		{name: "official_sse", baseURL: "https://chatgpt.com/backend-api/codex#"},
		{name: "relay_json", baseURL: "https://relay.example"},
	} {
		for _, raw := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/raw=%t", scenario.name, raw), func(t *testing.T) {
				client := enttest.NewEntClient(t, "sqlite3", fmt.Sprintf("file:portable-agent-http-%s-%t?mode=memory&_fk=0", scenario.name, raw))
				t.Cleanup(func() { client.Close() })
				ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
				service := biz.NewSystemService(biz.SystemServiceParams{Ent: client})
				require.NoError(t, service.SetSecretKey(ctx, "test-agent-message-installation-secret"))
				request := rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse)
				var err error
				request.Body, err = sjson.SetBytes([]byte(portableAgentToolFixture), "stream", false)
				require.NoError(t, err)
				events := portableAgentEvents(t)
				responseBody := []byte(gjson.GetBytes(events[len(events)-1].Data, "response").Raw)
				executor := &responsesReasoningPipelineExecutor{
					events: events,
					response: &httpclient.Response{StatusCode: 200, Body: responseBody, Headers: http.Header{
						"Content-Type":   []string{"application/json"},
						"Content-Length": []string{fmt.Sprint(len(responseBody))},
					}},
				}
				provider, err := codex.NewOutboundTransformer(codex.Params{
					BaseURL:       scenario.baseURL,
					TokenProvider: oauth.NewStaticTokenProvider(&oauth.OAuthCredentials{AccessToken: t.Name()}),
				})
				require.NoError(t, err)
				_, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, t.Name(), raw, 0,
					func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
						state.APIKey = &ent.APIKey{ID: 1, ProjectID: 1}
						state.ChannelModelsCandidates[0].Channel.Outbound = provider
						return portableResponsesAgentTransport(outbound, service)
					},
				)
				require.NoError(t, err)
				require.Len(t, executor.requests, 1)
				require.False(t, gjson.GetBytes(executor.requests[0].Body, "input.0.tools.0.tools.0.parameters.properties.message.encrypted").Bool())
				require.False(t, result.Stream)
				require.NotContains(t, string(result.Response.Body), "Exact portable agent message")
				arguments := gjson.GetBytes(result.Response.Body, "output.0.arguments").String()
				require.Contains(t, gjson.Get(arguments, "message").String(), responsesAgentMessagePrefix)
				require.Equal(t, "call_portable", gjson.GetBytes(result.Response.Body, "output.0.call_id").String())
				if raw {
					require.Equal(t, "message", gjson.GetBytes(result.Response.Body, "output.0.encrypted_function_args.0").String())
				}
				require.Empty(t, result.Response.Headers.Get("Content-Length"))
			})
		}
	}
}
