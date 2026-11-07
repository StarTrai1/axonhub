package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
)

func TestQuotaAdmissionFailurePersistsWithoutExecution(t *testing.T) {
	ctx, client := setupTest(t)
	svc := createTestRequestService(t, client)
	record, err := client.Request.Create().SetModelID("gpt-6.1-terra").SetRequestBody([]byte(`{}`)).SetStatus(request.StatusProcessing).Save(ctx)
	require.NoError(t, err)
	state := &PersistenceState{Request: record, RequestService: svc}
	require.NoError(t, persistQuotaAdmissionFailure(ctx, state, newQuotaReserveUnavailableError("gpt-6.1-terra")))
	stored, err := client.Request.Get(ctx, record.ID)
	require.NoError(t, err)
	require.Equal(t, request.StatusFailed, stored.Status)
	require.Equal(t, "quota_reserve_unavailable", gjson.GetBytes(stored.ResponseBody, "error.code").String())
	require.Equal(t, int64(503), gjson.GetBytes(stored.ResponseBody, "status_code").Int())
	count, err := client.RequestExecution.Query().Where(requestexecution.RequestIDEQ(record.ID)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count, "a local rejection must not fabricate an upstream attempt")
}
