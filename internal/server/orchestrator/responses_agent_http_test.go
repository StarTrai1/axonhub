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
	"github.com/looplj/axonhub/llm/pipeline"
)

func TestResponsesAgentTransportNonStreaming(t *testing.T) {
	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprintf("raw=%t", raw), func(t *testing.T) {
			client := enttest.NewEntClient(t, "sqlite3", fmt.Sprintf("file:portable-agent-http-%t?mode=memory&_fk=0", raw))
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
			executor := &responsesReasoningPipelineExecutor{response: &httpclient.Response{StatusCode: 200, Body: responseBody, Headers: http.Header{"Content-Length": []string{"1"}}}}
			_, result, err := runRejectedReasoningPipeline(t, ctx, request, executor, t.Name(), raw, 0,
				func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
					state.APIKey = &ent.APIKey{ID: 1, ProjectID: 1}
					return portableResponsesAgentTransport(outbound, service)
				},
			)
			require.NoError(t, err)
			require.False(t, result.Stream)
			require.NotContains(t, string(result.Response.Body), "Exact portable agent message")
			arguments := gjson.GetBytes(result.Response.Body, "output.0.arguments").String()
			require.Contains(t, gjson.Get(arguments, "message").String(), responsesAgentMessagePrefix)
			require.Empty(t, result.Response.Headers.Get("Content-Length"))
		})
	}
}
