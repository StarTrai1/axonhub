package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestRemoteCompactionRestoresWebSocketSnapshotAfterRestartAndModelSwitch(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:compaction-restart?mode=memory&_fk=0")
	t.Cleanup(func() { client.Close() })
	ctx := authz.WithTestBypass(ent.NewContext(context.Background(), client))
	_, err := client.DataStorage.Create().SetName("primary").SetDescription("test storage").SetPrimary(true).SetType("database").SetSettings(new(objects.DataStorageSettings)).Save(ctx)
	require.NoError(t, err)
	apiKey := &ent.APIKey{ID: 71, ProjectID: 72}
	ctx = contexts.WithProjectID(contexts.WithAPIKey(ctx, apiKey), apiKey.ProjectID)
	service := createTestRequestService(t, client)
	ref := &remoteCompactionReference{ID: "cmp_axonhub_retained", EncryptedContent: "axonhub-local-v1.retained"}
	cacheKey := remoteCompactionCacheKey(ref)
	stored, err := client.Request.Create().
		SetProjectID(apiKey.ProjectID).
		SetAPIKeyID(apiKey.ID).
		SetModelID("gpt-5.6-sol").
		SetFormat(llm.APIFormatOpenAIResponseWebSocket.String()).
		SetStatus(request.StatusCompleted).
		SetStream(true).
		SetCreatedAt(time.Now().Add(-48 * time.Hour)).
		SetRequestHeaders(objects.JSONRawMessage(`{"Thread-Id":["thread-before-restart"],"X-Axonhub-Remote-Compaction-Cache":["` + cacheKey + `"]}`)).
		SetRequestBody(objects.JSONRawMessage(`{"model":"gpt-5.6-sol","input":[{"role":"user","content":"source history"}]}`)).
		SetResponseBody(objects.JSONRawMessage(`{"id":"resp_compaction","status":"completed","output":[{"type":"compaction","id":"cmp_axonhub_retained"}]}`)).
		Save(ctx)
	require.NoError(t, err)
	for _, status := range []requestexecution.Status{requestexecution.StatusCompleted, requestexecution.StatusFailed} {
		text := "retained summary without regeneration"
		if status == requestexecution.StatusFailed {
			text = "must not restore a failed attempt"
		}
		_, err = client.RequestExecution.Create().
			SetRequestID(stored.ID).
			SetProjectID(apiKey.ProjectID).
			SetModelID("gpt-5.6-sol").
			SetFormat(llm.APIFormatOpenAIResponse.String()).
			SetStatus(status).
			SetStream(true).
			SetRequestBody(objects.JSONRawMessage(`{"input":"source history"}`)).
			SetResponseBody(objects.JSONRawMessage(`{"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"` + text + `"}]}]}`)).
			Save(ctx)
		require.NoError(t, err)
	}
	adapter := newRemoteCompactionAdapter(service, nil, nil)
	for range 2 {
		summary, loadErr := adapter.summaryForCompaction(ctx, cacheKey, ref, "thread-after-resume", "gpt-6-astra", &PersistenceState{APIKey: apiKey}, nil)
		require.NoError(t, loadErr)
		require.Equal(t, "retained summary without regeneration", summary)
	}
	restarted := newRemoteCompactionAdapter(service, nil, nil)
	summary, loadErr := restarted.summaryForCompaction(ctx, cacheKey, ref, "thread-after-resume", "gpt-6-astra", &PersistenceState{APIKey: apiKey}, nil)
	require.NoError(t, loadErr)
	require.Equal(t, "retained summary without regeneration", summary)
	for _, other := range []*ent.APIKey{{ID: apiKey.ID + 1, ProjectID: apiKey.ProjectID}, {ID: apiKey.ID, ProjectID: apiKey.ProjectID + 1}} {
		_, loadErr := adapter.summaryForCompaction(ctx, cacheKey, ref, "thread-after-resume", "gpt-6-astra", &PersistenceState{APIKey: other}, nil)
		var responseErr *llm.ResponseError
		require.ErrorAs(t, loadErr, &responseErr)
		require.Equal(t, http.StatusBadRequest, responseErr.StatusCode)
		require.Equal(t, "compaction_history_unavailable", responseErr.Detail.Code)
	}
}

func TestRemoteCompactionPreparationFailureIsPersistedWithoutUpstreamExecution(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:compaction-failure?mode=memory&_fk=0")
	t.Cleanup(func() { client.Close() })
	ctx := authz.WithTestBypass(ent.NewContext(context.Background(), client))
	_, err := client.DataStorage.Create().SetName("primary").SetDescription("test storage").SetPrimary(true).SetType("database").SetSettings(new(objects.DataStorageSettings)).Save(ctx)
	require.NoError(t, err)
	apiKey := &ent.APIKey{ID: 81, ProjectID: 82}
	ctx = shared.WithResponsesWebSocket(contexts.WithProjectID(contexts.WithAPIKey(ctx, apiKey), apiKey.ProjectID))
	state := &PersistenceState{
		APIKey: apiKey, RequestService: createTestRequestService(t, client),
		LlmRequest: &llm.Request{Model: "gpt-6-astra", APIFormat: llm.APIFormatOpenAIResponse},
		RawRequest: &httpclient.Request{Body: []byte(`{"model":"gpt-6-astra","input":[{"type":"compaction","id":"cmp_missing"}]}`)},
	}
	inbound := &PersistentInboundTransformer{state: state, wrapped: responses.NewInboundTransformer()}
	require.NoError(t, persistRemoteCompactionPreparationFailure(ctx, inbound, errors.New("unrelated error")))
	require.Nil(t, state.Request)
	failure := &remoteCompactionPreparationError{cause: &llm.ResponseError{StatusCode: http.StatusBadRequest, Detail: llm.ErrorDetail{Code: "compaction_history_unavailable", Type: "invalid_request_error", Param: "input", Message: "Retained source is unavailable"}}}
	require.NoError(t, persistRemoteCompactionPreparationFailure(ctx, inbound, failure))
	require.NotNil(t, state.Request)
	stored, err := client.Request.Get(ctx, state.Request.ID)
	require.NoError(t, err)
	require.Equal(t, request.StatusFailed, stored.Status)
	require.Equal(t, llm.APIFormatOpenAIResponseWebSocket.String(), stored.Format)
	require.Equal(t, apiKey.ID, stored.APIKeyID)
	require.Equal(t, apiKey.ProjectID, stored.ProjectID)
	body, err := biz.DecodeStoredPayload(stored.ResponseBody)
	require.NoError(t, err)
	require.Equal(t, "compaction_history_unavailable", gjson.GetBytes(body, "error.code").String())
	require.NoError(t, persistRemoteCompactionPreparationFailure(ctx, inbound, failure))
	count, err := client.Request.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	count, err = client.RequestExecution.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}
