package biz

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/pkg/xcache"
)

func TestRequestService_MetadataUpdatesDoNotReadPayloads(t *testing.T) {
	cache := xcache.Config{Mode: xcache.ModeMemory}
	ctx, client, svc := newRequestStickyTestService(t, cache, cache)
	_, trace, channel := createRequestStickyScope(t, ctx, client)
	body := []byte(`{"input":"` + strings.Repeat("conversation ", 8192) + `"}`)
	req, err := client.Request.Create().SetProjectID(trace.ProjectID).SetTraceID(trace.ID).
		SetModelID("gpt-6-astra").SetRequestBody(body).SetResponseBody(body).
		SetStatus(request.StatusProcessing).Save(ctx)
	require.NoError(t, err)
	execution, err := client.RequestExecution.Create().SetProjectID(trace.ProjectID).SetRequestID(req.ID).
		SetModelID("gpt-6-astra").SetRequestBody(body).SetResponseBody(body).
		SetStatus(requestexecution.StatusProcessing).Save(ctx)
	require.NoError(t, err)

	// Inspect the actual entities returned by Ent, including update readbacks.
	// Metadata operations must not hydrate the large JSON columns at either stage.
	checking := true
	check := func(value ent.Value) {
		if !checking {
			return
		}
		switch v := value.(type) {
		case *ent.Request:
			require.Empty(t, v.RequestBody)
			require.Empty(t, v.ResponseBody)
			require.Empty(t, v.ResponseChunks)
		case []*ent.Request:
			for _, row := range v {
				require.Empty(t, row.RequestBody)
				require.Empty(t, row.ResponseBody)
				require.Empty(t, row.ResponseChunks)
			}
		case *ent.RequestExecution:
			require.Empty(t, v.RequestBody)
			require.Empty(t, v.ResponseBody)
			require.Empty(t, v.ResponseChunks)
		case []*ent.RequestExecution:
			for _, row := range v {
				require.Empty(t, row.RequestBody)
				require.Empty(t, row.ResponseBody)
				require.Empty(t, row.ResponseChunks)
			}
		}
	}
	client.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
			value, err := next.Mutate(ctx, m)
			if err == nil {
				check(value)
			}
			return value, err
		})
	})
	client.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, q ent.Query) (ent.Value, error) {
			value, err := next.Query(ctx, q)
			if err == nil {
				check(value)
			}
			return value, err
		})
	}))

	require.NoError(t, svc.UpdateRequestChannelID(ctx, req.ID, channel.ID))
	headers := http.Header{"X-Request-Id": {"upstream-id"}}
	require.NoError(t, svc.UpdateRequestResponseHeaders(ctx, req.ID, headers))
	require.NoError(t, svc.UpdateRequestExecutionResponseHeaders(ctx, execution.ID, headers))
	require.NoError(t, svc.UpdateRequestStatus(ctx, req.ID, request.StatusFailed))
	require.NoError(t, svc.UpdateRequestExecutionStatus(ctx, execution.ID, requestexecution.StatusFailed, "temporary failure", nil))
	response := map[string]string{"id": "response-completed"}
	require.NoError(t, svc.UpdateRequestFinalized(ctx, req.ID, request.StatusCompleted, "response-completed", response, nil))
	require.NoError(t, svc.UpdateRequestExecutionFinalized(ctx, execution.ID, requestexecution.StatusCompleted, "", "response-completed", response, nil, "gpt-6-astra"))

	checking = false
	stored, err := client.Request.Get(ctx, req.ID)
	require.NoError(t, err)
	require.Equal(t, body, []byte(stored.RequestBody), "projection must not change retained history")
	require.Equal(t, request.StatusCompleted, stored.Status)
	require.Equal(t, channel.ID, stored.ChannelID)
	require.JSONEq(t, `{"id":"response-completed"}`, string(stored.ResponseBody))
	require.Contains(t, string(stored.ResponseHeaders), "upstream-id")
	previous, err := svc.GetPreviousChannelID(ctx, trace.ID)
	require.NoError(t, err)
	require.Equal(t, channel.ID, previous)
	storedExecution, err := client.RequestExecution.Get(ctx, execution.ID)
	require.NoError(t, err)
	require.Equal(t, body, []byte(storedExecution.RequestBody))
	require.Equal(t, requestexecution.StatusCompleted, storedExecution.Status)
}

func BenchmarkRequestChannelSelectionMetadata(b *testing.B) {
	client := enttest.NewEntClient(b, "sqlite3", "file:metadata_benchmark_"+time.Now().Format("150405.000000000")+"?mode=memory&_fk=0")
	b.Cleanup(func() { client.Close() })
	ctx := authz.WithTestBypass(ent.NewContext(b.Context(), client))
	body := []byte(`{"input":"` + strings.Repeat("x", 1024*1024) + `"}`)
	req, err := client.Request.Create().SetModelID("gpt-6-astra").SetRequestBody(body).
		SetResponseBody(body).SetStatus(request.StatusProcessing).Save(ctx)
	if err != nil {
		b.Fatal(err)
	}
	svc := &RequestService{AbstractService: &AbstractService{db: client}}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := svc.UpdateRequestChannelID(ctx, req.ID, 0); err != nil {
			b.Fatal(err)
		}
	}
}
