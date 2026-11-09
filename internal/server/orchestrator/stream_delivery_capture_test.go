package orchestrator

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestStreamDeliveryCapturedTerminalSurvivesCanceledDrain(t *testing.T) {
	for _, status := range []string{"completed", "failed", "incomplete"} {
		t.Run(status, func(t *testing.T) {
			client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
			project := createTestProject(t, ctx, client)
			channel := createTestChannel(t, ctx, client)
			_, requestService, _, usageService := setupTestServices(t, client)
			req, err := client.Request.Create().SetProjectID(project.ID).SetChannelID(channel.ID).
				SetModelID("gpt-5").SetStatus(request.StatusProcessing).SetStream(true).
				SetRequestBody([]byte(`{"stream":true}`)).Save(ctx)
			require.NoError(t, err)
			execution, err := client.RequestExecution.Create().SetRequestID(req.ID).SetProjectID(project.ID).
				SetChannelID(channel.ID).SetModelID("gpt-5").SetFormat(llm.APIFormatOpenAIResponse.String()).
				SetStatus(requestexecution.StatusProcessing).SetStream(true).
				SetRequestBody([]byte(`{"stream":true}`)).Save(ctx)
			require.NoError(t, err)
			outbound, err := responses.NewOutboundTransformer("https://example.test", "synthetic-key")
			require.NoError(t, err)
			captured := &passThroughStreamError{}
			state := &PersistenceState{RawStreamErrRef: captured}
			streamCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			stream := NewOutboundPersistentStream(streamCtx, streams.SliceStream([]*httpclient.StreamEvent{}), req, execution, requestService, usageService, outbound, nil, state)
			event := &httpclient.StreamEvent{
				Type: "response." + status,
				Data: []byte(fmt.Sprintf(`{"type":"response.%s","response":{"id":"resp_captured","status":%q,"output":[],"usage":{"input_tokens":12,"output_tokens":34,"total_tokens":46}}}`, status, status)),
			}
			// The raw reader saw the terminal, but cancellation prevents the
			// transformed consumer from reaching Next/Current for that event.
			captured.recordTerminal(event)
			cancel()
			state.RawStreamErrRef = &passThroughStreamError{}
			require.NoError(t, stream.Close(), "finalization uses its own attempt's evidence")
			saved, err := client.RequestExecution.Get(ctx, execution.ID)
			require.NoError(t, err)
			want := requestexecution.StatusFailed
			if status == "completed" {
				want = requestexecution.StatusCompleted
				require.Empty(t, saved.ErrorMessage)
			}
			require.Equal(t, want, saved.Status)
			logs, err := client.UsageLog.Query().All(ctx)
			require.NoError(t, err)
			require.Len(t, logs, 1)
			require.EqualValues(t, 46, logs[0].TotalTokens)
			require.False(t, state.DeliveryAcknowledged, "provider evidence must not acknowledge downstream delivery")
		})
	}
}
