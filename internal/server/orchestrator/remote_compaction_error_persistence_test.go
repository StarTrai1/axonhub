package orchestrator

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/oauth"
	"github.com/looplj/axonhub/llm/transformer/openai/codex"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestResponsesRejectedLocalCompactionPersistsRetriesAndProviderErrors(t *testing.T) {
	for _, exhausted := range []bool{false, true} {
		name := "recovers after capacity error"
		if exhausted {
			name = "capacity retry budget exhausted"
		}
		t.Run(name, func(t *testing.T) {
			client := enttest.NewEntClient(t, "sqlite3", "file:compaction-errors?mode=memory&_fk=0")
			t.Cleanup(func() { client.Close() })
			ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
			apiKey := &ent.APIKey{ID: 111, ProjectID: 112}
			ctx = contexts.WithProjectID(contexts.WithAPIKey(ctx, apiKey), apiKey.ProjectID)
			_, err := client.DataStorage.Create().SetName("primary").SetDescription("synthetic test storage").SetPrimary(true).SetType("database").SetSettings(new(objects.DataStorageSettings)).Save(ctx)
			require.NoError(t, err)
			service := createTestRequestService(t, client)
			system := biz.NewSystemService(biz.SystemServiceParams{Ent: client})
			provider, err := codex.NewOutboundTransformer(codex.Params{
				BaseURL: "https://example.invalid/v1",
				TokenProvider: oauth.NewStaticTokenProvider(&oauth.OAuthCredentials{
					AccessToken: "synthetic-bridge-credential",
				}),
			})
			require.NoError(t, err)
			candidate := &ChannelModelsCandidate{
				Channel: &biz.Channel{
					Channel: &ent.Channel{
						ID: 94060, Name: "bridge-test", Type: entchannel.TypeCodex,
						Settings: &objects.ChannelSettings{PassThroughBody: lo.ToPtr(true)},
					},
					Outbound: provider,
				},
				Models:    []biz.ChannelModelEntry{{RequestModel: "gpt-6-astra", ActualModel: "gpt-6-astra", Source: "direct"}},
				APIFormat: llm.APIFormatOpenAIResponse.String(),
			}
			state := &PersistenceState{
				APIKey: apiKey, ChannelModelsCandidates: []*ChannelModelsCandidate{candidate},
				RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxSingleChannelRetries: 1}},
			}
			executor := &localCompactionRetryExecutor{
				failures: []error{bridgeOverloadError()},
				events: []*httpclient.StreamEvent{{
					Type: "response.completed",
					Data: []byte(`{"type":"response.completed","response":{"id":"resp_bridge","object":"response","status":"completed","model":"gpt-6-astra","output":[{"type":"message","id":"msg_bridge","role":"assistant","content":[{"type":"output_text","text":"complete bridge summary"}]}]}}`),
				}},
			}
			if exhausted {
				executor.failures = append(executor.failures, bridgeOverloadError())
			}
			adapter := newRemoteCompactionAdapter(service, nil, system)
			summary, err := adapter.generateLocalSummary(ctx, "synthetic-compaction-cache-key", &remoteCompactionSource{
				body:    []byte(`{"model":"gpt-6-astra","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"complete original history"}]},{"type":"compaction_trigger"}]}`),
				headers: http.Header{"Thread-Id": {"synthetic-bridge-thread"}},
			}, state, executor)
			require.Len(t, executor.bodies, 2, "summary execution returned: %v", err)
			require.Equal(t, executor.bodies[0], executor.bodies[1])
			stored, loadErr := client.Request.Query().Only(ctx)
			require.NoError(t, loadErr)
			require.Equal(t, apiKey.ID, stored.APIKeyID)
			require.Equal(t, apiKey.ProjectID, stored.ProjectID)
			executions, loadErr := client.RequestExecution.Query().Order(ent.Asc(requestexecution.FieldID)).All(ctx)
			require.NoError(t, loadErr)
			require.Len(t, executions, 2)
			require.Equal(t, requestexecution.StatusFailed, executions[0].Status)
			require.Equal(t, lo.ToPtr(http.StatusInternalServerError), executions[0].ResponseStatusCode)
			require.Equal(t, "model capacity reached", executions[0].ErrorMessage)
			body, loadErr := biz.DecodeStoredPayload(stored.ResponseBody)
			require.NoError(t, loadErr)
			if exhausted {
				require.Error(t, err)
				require.Empty(t, summary)
				require.Equal(t, request.StatusFailed, stored.Status)
				require.Equal(t, requestexecution.StatusFailed, executions[1].Status)
				require.Equal(t, lo.ToPtr(http.StatusInternalServerError), executions[1].ResponseStatusCode)
				require.Equal(t, "model capacity reached", gjson.GetBytes(body, "error.message").String())
				require.Equal(t, "model_overloaded", gjson.GetBytes(body, "error.code").String())
				clientError := responses.NewInboundTransformer().TransformError(ctx, &remoteCompactionPreparationError{cause: err})
				require.Equal(t, http.StatusInternalServerError, clientError.StatusCode)
				require.JSONEq(t, string(body), string(clientError.Body))
			} else {
				require.NoError(t, err)
				require.Equal(t, "complete bridge summary", summary)
				require.Equal(t, request.StatusCompleted, stored.Status)
				require.Equal(t, candidate.Channel.ID, stored.ChannelID)
				require.Equal(t, requestexecution.StatusCompleted, executions[1].Status)
				require.Equal(t, "complete bridge summary", gjson.GetBytes(body, "output.0.content.0.text").String())
			}
		})
	}
}

func TestResponsesRejectedLocalCompactionPersistsClassifiedFailures(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:compaction-failure-status?mode=memory&_fk=0")
	t.Cleanup(func() { client.Close() })
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	stored, err := client.Request.Create().SetProjectID(1).SetModelID("gpt-6-astra").SetFormat(llm.APIFormatOpenAIResponse.String()).SetRequestBody(objects.JSONRawMessage(`{}`)).SetStatus(request.StatusProcessing).SetStream(true).Save(ctx)
	require.NoError(t, err)
	adapter := newRemoteCompactionAdapter(createTestRequestService(t, client), nil, nil)
	for _, scenario := range []struct {
		name   string
		err    error
		status requestexecution.Status
		code   int
	}{
		{"opening load error", bridgeOverloadError(), requestexecution.StatusFailed, 500},
		{"EOF during summary", io.ErrUnexpectedEOF, requestexecution.StatusFailed, 502},
		{"canceled request", context.Canceled, requestexecution.StatusCanceled, 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			execution, err := client.RequestExecution.Create().SetRequestID(stored.ID).SetProjectID(1).SetModelID("gpt-6-astra").SetFormat(llm.APIFormatOpenAIResponse.String()).SetRequestBody(objects.JSONRawMessage(`{}`)).SetStatus(requestexecution.StatusProcessing).SetStream(true).Save(ctx)
			require.NoError(t, err)
			adapter.markBridgeExecutionFailed(ctx, execution, scenario.err)
			updated, err := client.RequestExecution.Get(ctx, execution.ID)
			require.NoError(t, err)
			require.Equal(t, scenario.status, updated.Status)
			if scenario.code == 0 {
				require.Nil(t, updated.ResponseStatusCode)
			} else {
				require.Equal(t, lo.ToPtr(scenario.code), updated.ResponseStatusCode)
			}
			require.Equal(t, ExtractErrorMessage(ClassifyUpstreamTransportError(scenario.err)), updated.ErrorMessage)
		})
	}
}
