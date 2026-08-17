package biz

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent/project"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
)

func TestRequestService_LoadReferencedExecutionRequestBody(t *testing.T) {
	svc, client, ctx := setupTestRequestService(t)
	defer client.Close()

	proj, err := client.Project.Create().
		SetName("referenced-execution-body").
		SetStatus(project.StatusActive).
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
		SetStatus(requestexecution.StatusCompleted).
		SetStream(true).
		Save(ctx)
	require.NoError(t, err)

	loaded, err := svc.LoadRequestExecutionRequestBody(ctx, execution)
	require.NoError(t, err)
	require.Equal(t, raw, []byte(loaded))
}
