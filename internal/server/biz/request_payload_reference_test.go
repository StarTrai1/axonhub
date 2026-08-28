package biz

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent/datastorage"
	"github.com/looplj/axonhub/internal/ent/project"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/objects"
)

func TestRequestService_LoadReferencedExecutionRequestBody(t *testing.T) {
	svc, client, ctx := setupTestRequestService(t)
	defer client.Close()

	proj, err := client.Project.Create().
		SetName("referenced-execution-body").
		SetStatus(project.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	primaryStorage, err := client.DataStorage.Create().
		SetName("primary-database").
		SetDescription("primary test database").
		SetPrimary(true).
		SetType(datastorage.TypeDatabase).
		SetSettings(&objects.DataStorageSettings{}).
		Save(ctx)
	require.NoError(t, err)

	raw := []byte("{\"input\":\"" + strings.Repeat("shared request history ", 8192) + "\"}")
	storedParent, err := CompressStoredPayload(raw)
	require.NoError(t, err)

	parent, err := client.Request.Create().
		SetProjectID(proj.ID).
		SetModelID("gpt-5.6-sol").
		SetFormat("openai/responses").
		SetRequestBody(storedParent).
		SetDataStorageID(primaryStorage.ID).
		SetStatus(request.StatusCompleted).
		SetStream(true).
		Save(ctx)
	require.NoError(t, err)

	reference, referenced, err := referenceStoredRequestBody(parent.ID, storedParent, raw)
	require.NoError(t, err)
	require.True(t, referenced)

	execution, err := client.RequestExecution.Create().
		SetProjectID(proj.ID).
		SetRequestID(parent.ID).
		SetModelID("gpt-5.6-sol").
		SetFormat("openai/responses").
		SetRequestBody(reference).
		SetDataStorageID(primaryStorage.ID).
		SetStatus(requestexecution.StatusCompleted).
		SetStream(true).
		Save(ctx)
	require.NoError(t, err)

	loaded, err := svc.LoadRequestExecutionRequestBody(ctx, execution)
	require.NoError(t, err)
	require.Equal(t, raw, []byte(loaded))
}

func TestRequestService_LoadReferencedExecutionRequestBodyWithDatabaseJSONEscapes(t *testing.T) {
	svc, client, ctx := setupTestRequestService(t)
	defer client.Close()

	proj, err := client.Project.Create().
		SetName("referenced-execution-body-html-escapes").
		SetStatus(project.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	primaryStorage, err := client.DataStorage.Create().
		SetName("primary-database-html-escapes").
		SetDescription("primary test database").
		SetPrimary(true).
		SetType(datastorage.TypeDatabase).
		SetSettings(&objects.DataStorageSettings{}).
		Save(ctx)
	require.NoError(t, err)

	raw := []byte(`{"input":"` + strings.Repeat("<request>&response>", 4096) + `"}`)
	parent, err := client.Request.Create().
		SetProjectID(proj.ID).
		SetModelID("gpt-5.6-sol").
		SetFormat("openai/responses").
		SetRequestBody(raw).
		SetDataStorageID(primaryStorage.ID).
		SetStatus(request.StatusCompleted).
		SetStream(true).
		Save(ctx)
	require.NoError(t, err)

	reference, referenced, err := referenceStoredRequestBody(parent.ID, parent.RequestBody, raw)
	require.NoError(t, err)
	require.True(t, referenced)

	execution, err := client.RequestExecution.Create().
		SetProjectID(proj.ID).
		SetRequestID(parent.ID).
		SetModelID("gpt-5.6-sol").
		SetFormat("openai/responses").
		SetRequestBody(reference).
		SetDataStorageID(primaryStorage.ID).
		SetStatus(requestexecution.StatusCompleted).
		SetStream(true).
		Save(ctx)
	require.NoError(t, err)

	loaded, err := svc.LoadRequestExecutionRequestBody(ctx, execution)
	require.NoError(t, err)
	persisted, err := json.Marshal(objects.JSONRawMessage(raw))
	require.NoError(t, err)
	require.Equal(t, persisted, []byte(loaded))
}

func TestValidateStoredRequestBodyReferenceRepairsLegacyDatabaseJSONEscapes(t *testing.T) {
	raw := []byte(`{"input":"<request>&response>"}`)
	digest := sha256.Sum256(raw)
	reference := databaseRequestBodyReferenceEnvelope{
		RawBytes: len(raw),
		SHA256:   hex.EncodeToString(digest[:]),
	}
	persisted, err := json.Marshal(objects.JSONRawMessage(raw))
	require.NoError(t, err)
	require.NotEqual(t, raw, persisted)
	require.NoError(t, validateStoredRequestBodyReference(reference, persisted))
}

func TestRequestExternalIDAcceptsRelayResponseID(t *testing.T) {
	svc, client, ctx := setupTestRequestService(t)
	defer client.Close()

	proj, err := client.Project.Create().
		SetName("long-relay-response-id").
		SetStatus(project.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	primaryStorage, err := client.DataStorage.Create().
		SetName("primary-database-long-response-id").
		SetDescription("primary test database").
		SetPrimary(true).
		SetType(datastorage.TypeDatabase).
		SetSettings(&objects.DataStorageSettings{}).
		Save(ctx)
	require.NoError(t, err)

	parent, err := client.Request.Create().
		SetProjectID(proj.ID).
		SetModelID("gpt-5.6-sol").
		SetFormat("openai/responses").
		SetRequestBody(objects.JSONRawMessage(`{}`)).
		SetDataStorageID(primaryStorage.ID).
		SetStatus(request.StatusProcessing).
		SetStream(true).
		Save(ctx)
	require.NoError(t, err)

	execution, err := client.RequestExecution.Create().
		SetProjectID(proj.ID).
		SetRequestID(parent.ID).
		SetModelID("gpt-5.6-sol").
		SetFormat("openai/responses").
		SetRequestBody(objects.JSONRawMessage(`{}`)).
		SetDataStorageID(primaryStorage.ID).
		SetStatus(requestexecution.StatusProcessing).
		SetStream(true).
		Save(ctx)
	require.NoError(t, err)

	externalID := "resp_" + strings.Repeat("x", 1104)
	require.NoError(t, svc.UpdateRequestExecutionCompleted(ctx, execution.ID, externalID, []byte(`{"id":"ok"}`), nil))
	require.NoError(t, svc.UpdateRequestCompleted(ctx, parent.ID, externalID, []byte(`{"id":"ok"}`), nil))

	updatedExecution, err := client.RequestExecution.Get(ctx, execution.ID)
	require.NoError(t, err)
	require.Equal(t, requestexecution.StatusCompleted, updatedExecution.Status)
	require.Equal(t, externalID, updatedExecution.ExternalID)
	updatedParent, err := client.Request.Get(ctx, parent.ID)
	require.NoError(t, err)
	require.Equal(t, request.StatusCompleted, updatedParent.Status)
	require.Equal(t, externalID, updatedParent.ExternalID)
}
