package biz

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent/project"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
)

func TestRequestService_FindRecentCompletedRequestMetadata(t *testing.T) {
	svc, client, ctx := setupTestRequestService(t)
	defer client.Close()

	proj, err := client.Project.Create().
		SetName("recent-request-metadata").
		SetStatus(project.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	apiKey, err := client.APIKey.Create().
		SetProjectID(proj.ID).
		SetKey("recent-request-metadata-key").
		SetName("recent-request-metadata").
		Save(ctx)
	require.NoError(t, err)

	older, err := client.Request.Create().
		SetProjectID(proj.ID).
		SetAPIKeyID(apiKey.ID).
		SetModelID("gpt-5.6-sol").
		SetFormat(llm.APIFormatOpenAIResponse.String()).
		SetRequestHeaders(objects.JSONRawMessage(`{"Thread-Id":["thread-1"]}`)).
		SetRequestBody(objects.JSONRawMessage(`{"input":[{"type":"message"}]}`)).
		SetStatus(request.StatusCompleted).
		SetStream(true).
		SetCreatedAt(time.Now().UTC().Add(-time.Minute)).
		Save(ctx)
	require.NoError(t, err)

	newer, err := client.Request.Create().
		SetProjectID(proj.ID).
		SetAPIKeyID(apiKey.ID).
		SetModelID("gpt-5.6-sol").
		SetFormat(llm.APIFormatOpenAIResponse.String()).
		SetRequestHeaders(objects.JSONRawMessage(`{"Thread-Id":["thread-2"]}`)).
		SetRequestBody(objects.JSONRawMessage(`{"input":[{"type":"message"},{"type":"compaction_trigger"}]}`)).
		SetStatus(request.StatusCompleted).
		SetStream(true).
		SetCreatedAt(time.Now().UTC()).
		Save(ctx)
	require.NoError(t, err)

	metadata, err := svc.FindRecentCompletedRequestMetadata(
		ctx,
		apiKey.ID,
		proj.ID,
		"gpt-5.6-sol",
		llm.APIFormatOpenAIResponse,
		10,
	)
	require.NoError(t, err)
	require.Len(t, metadata, 2)
	require.Equal(t, newer.ID, metadata[0].ID)
	require.JSONEq(t, `{"Thread-Id":["thread-2"]}`, string(metadata[0].RequestHeaders))
	require.Equal(t, older.ID, metadata[1].ID)

	loaded, err := svc.GetRequestByID(ctx, metadata[0].ID)
	require.NoError(t, err)
	require.JSONEq(t, `{"input":[{"type":"message"},{"type":"compaction_trigger"}]}`, string(loaded.RequestBody))
}
