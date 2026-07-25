package gc

import (
	"context"
	"fmt"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/datastorage"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/ent/trace"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/biz"
)

const maxPayloadCleanupBatchSize = 50

func (w *Worker) getPayloadCleanupBatchSize() int {
	return min(w.getBatchSize(), maxPayloadCleanupBatchSize)
}

func (w *Worker) cleanupRequestPayloads(ctx context.Context, cleanupDays int) error {
	if cleanupDays < 0 {
		return nil
	}

	cutoff := cleanupCutoff(cleanupDays)
	if _, err := w.clearRequestRecordPayloads(ctx, cutoff); err != nil {
		return err
	}
	if _, err := w.clearExecutionRequestPayloads(ctx, cutoff); err != nil {
		return err
	}

	return nil
}

func (w *Worker) cleanupResponsePayloads(ctx context.Context, cleanupDays int) error {
	if cleanupDays < 0 {
		return nil
	}

	cutoff := cleanupCutoff(cleanupDays)
	if _, err := w.clearRequestRecordResponses(ctx, cutoff); err != nil {
		return err
	}
	if _, err := w.clearExecutionResponses(ctx, cutoff); err != nil {
		return err
	}

	return nil
}

func (w *Worker) clearRequestRecordPayloads(ctx context.Context, cutoff time.Time) (int, error) {
	batchSize := w.getPayloadCleanupBatchSize()
	totalUpdated := 0
	cache := make(map[int]*ent.DataStorage)
	lastID := 0

	for {
		records, err := w.Ent.Request.Query().
			Select(request.FieldID, request.FieldProjectID, request.FieldDataStorageID).
			Where(
				request.IDGT(lastID),
				request.CreatedAtLT(cutoff),
				request.StatusNotIn(request.StatusPending, request.StatusProcessing),
				request.Not(request.HasTraceWith(trace.StatusEQ(trace.StatusRetained))),
				request.Not(request.HasExecutionsWith(
					requestexecution.StatusIn(requestexecution.StatusPending, requestexecution.StatusProcessing),
				)),
				request.Or(
					request.RequestHeadersNotNil(),
					func(selector *entsql.Selector) {
						selector.Where(entsql.NEQ(selector.C(request.FieldRequestBody), "{}"))
					},
					request.HasDataStorageWith(datastorage.PrimaryEQ(false)),
				),
			).
			Order(ent.Asc(request.FieldID)).
			Limit(batchSize).
			All(ctx)
		if err != nil {
			return totalUpdated, fmt.Errorf("failed to query request payloads: %w", err)
		}
		if len(records) == 0 {
			break
		}

		ids := make([]int, 0, len(records))
		for _, record := range records {
			ids = append(ids, record.ID)
			w.cleanupRequestPayloadExternalStorage(ctx, record, cache, true)
		}
		lastID = ids[len(ids)-1]

		updated, err := w.Ent.Request.Update().
			Where(request.IDIn(ids...)).
			ClearRequestHeaders().
			Modify(func(builder *entsql.UpdateBuilder) {
				builder.Set(request.FieldRequestBody, "{}")
			}).
			Save(ctx)
		if err != nil {
			return totalUpdated, fmt.Errorf("failed to clear request payload batch: %w", err)
		}
		totalUpdated += updated
	}

	return totalUpdated, nil
}

func (w *Worker) clearExecutionRequestPayloads(ctx context.Context, cutoff time.Time) (int, error) {
	batchSize := w.getPayloadCleanupBatchSize()
	totalUpdated := 0
	cache := make(map[int]*ent.DataStorage)
	lastID := 0

	for {
		executions, err := w.Ent.RequestExecution.Query().
			Select(
				requestexecution.FieldID,
				requestexecution.FieldProjectID,
				requestexecution.FieldRequestID,
				requestexecution.FieldDataStorageID,
			).
			Where(
				requestexecution.IDGT(lastID),
				requestexecution.CreatedAtLT(cutoff),
				requestexecution.StatusNotIn(requestexecution.StatusPending, requestexecution.StatusProcessing),
				requestexecution.Not(requestexecution.HasRequestWith(
					request.HasTraceWith(trace.StatusEQ(trace.StatusRetained)),
				)),
				requestexecution.HasRequestWith(
					request.StatusNotIn(request.StatusPending, request.StatusProcessing),
					request.Not(request.HasExecutionsWith(
						requestexecution.StatusIn(requestexecution.StatusPending, requestexecution.StatusProcessing),
					)),
				),
				requestexecution.Or(
					requestexecution.RequestHeadersNotNil(),
					func(selector *entsql.Selector) {
						selector.Where(entsql.NEQ(selector.C(requestexecution.FieldRequestBody), "{}"))
					},
					requestexecution.HasDataStorageWith(datastorage.PrimaryEQ(false)),
				),
			).
			Order(ent.Asc(requestexecution.FieldID)).
			Limit(batchSize).
			All(ctx)
		if err != nil {
			return totalUpdated, fmt.Errorf("failed to query execution request payloads: %w", err)
		}
		if len(executions) == 0 {
			break
		}

		ids := make([]int, 0, len(executions))
		for _, execution := range executions {
			ids = append(ids, execution.ID)
			w.cleanupExecutionPayloadExternalStorage(ctx, execution, cache, true)
		}
		lastID = ids[len(ids)-1]

		updated, err := w.Ent.RequestExecution.Update().
			Where(requestexecution.IDIn(ids...)).
			ClearRequestHeaders().
			Modify(func(builder *entsql.UpdateBuilder) {
				builder.Set(requestexecution.FieldRequestBody, "{}")
			}).
			Save(ctx)
		if err != nil {
			return totalUpdated, fmt.Errorf("failed to clear execution request payload batch: %w", err)
		}
		totalUpdated += updated
	}

	return totalUpdated, nil
}

func (w *Worker) clearRequestRecordResponses(ctx context.Context, cutoff time.Time) (int, error) {
	batchSize := w.getPayloadCleanupBatchSize()
	totalUpdated := 0
	cache := make(map[int]*ent.DataStorage)
	lastID := 0

	for {
		records, err := w.Ent.Request.Query().
			Select(request.FieldID, request.FieldProjectID, request.FieldDataStorageID).
			Where(
				request.IDGT(lastID),
				request.CreatedAtLT(cutoff),
				request.StatusNotIn(request.StatusPending, request.StatusProcessing),
				request.Not(request.HasTraceWith(trace.StatusEQ(trace.StatusRetained))),
				request.Not(request.HasExecutionsWith(
					requestexecution.StatusIn(requestexecution.StatusPending, requestexecution.StatusProcessing),
				)),
				request.Or(
					request.ResponseBodyNotNil(),
					request.ResponseChunksNotNil(),
					request.HasDataStorageWith(datastorage.PrimaryEQ(false)),
				),
			).
			Order(ent.Asc(request.FieldID)).
			Limit(batchSize).
			All(ctx)
		if err != nil {
			return totalUpdated, fmt.Errorf("failed to query request responses: %w", err)
		}
		if len(records) == 0 {
			break
		}

		ids := make([]int, 0, len(records))
		for _, record := range records {
			ids = append(ids, record.ID)
			w.cleanupRequestPayloadExternalStorage(ctx, record, cache, false)
		}
		lastID = ids[len(ids)-1]

		updated, err := w.Ent.Request.Update().
			Where(request.IDIn(ids...)).
			ClearResponseBody().
			ClearResponseChunks().
			Save(ctx)
		if err != nil {
			return totalUpdated, fmt.Errorf("failed to clear request response batch: %w", err)
		}
		totalUpdated += updated
	}

	return totalUpdated, nil
}

func (w *Worker) clearExecutionResponses(ctx context.Context, cutoff time.Time) (int, error) {
	batchSize := w.getPayloadCleanupBatchSize()
	totalUpdated := 0
	cache := make(map[int]*ent.DataStorage)
	lastID := 0

	for {
		executions, err := w.Ent.RequestExecution.Query().
			Select(
				requestexecution.FieldID,
				requestexecution.FieldProjectID,
				requestexecution.FieldRequestID,
				requestexecution.FieldDataStorageID,
			).
			Where(
				requestexecution.IDGT(lastID),
				requestexecution.CreatedAtLT(cutoff),
				requestexecution.StatusNotIn(requestexecution.StatusPending, requestexecution.StatusProcessing),
				requestexecution.Not(requestexecution.HasRequestWith(
					request.HasTraceWith(trace.StatusEQ(trace.StatusRetained)),
				)),
				requestexecution.HasRequestWith(
					request.StatusNotIn(request.StatusPending, request.StatusProcessing),
					request.Not(request.HasExecutionsWith(
						requestexecution.StatusIn(requestexecution.StatusPending, requestexecution.StatusProcessing),
					)),
				),
				requestexecution.Or(
					requestexecution.ResponseBodyNotNil(),
					requestexecution.ResponseChunksNotNil(),
					requestexecution.HasDataStorageWith(datastorage.PrimaryEQ(false)),
				),
			).
			Order(ent.Asc(requestexecution.FieldID)).
			Limit(batchSize).
			All(ctx)
		if err != nil {
			return totalUpdated, fmt.Errorf("failed to query execution responses: %w", err)
		}
		if len(executions) == 0 {
			break
		}

		ids := make([]int, 0, len(executions))
		for _, execution := range executions {
			ids = append(ids, execution.ID)
			w.cleanupExecutionPayloadExternalStorage(ctx, execution, cache, false)
		}
		lastID = ids[len(ids)-1]

		updated, err := w.Ent.RequestExecution.Update().
			Where(requestexecution.IDIn(ids...)).
			ClearResponseBody().
			ClearResponseChunks().
			Save(ctx)
		if err != nil {
			return totalUpdated, fmt.Errorf("failed to clear execution response batch: %w", err)
		}
		totalUpdated += updated
	}

	return totalUpdated, nil
}

func (w *Worker) cleanupRequestPayloadExternalStorage(
	ctx context.Context,
	record *ent.Request,
	cache map[int]*ent.DataStorage,
	requestPayload bool,
) {
	if record == nil || record.DataStorageID == 0 || w.DataStorageService == nil {
		return
	}

	storage, err := w.getDataStorageCached(ctx, record.DataStorageID, cache)
	if err != nil {
		log.Warn(ctx, "Failed to load data storage while clearing request payload",
			log.Int("request_id", record.ID),
			log.Cause(err))
		return
	}
	if storage == nil || storage.Primary {
		return
	}

	keys := []string{biz.GenerateRequestBodyKey(record.ProjectID, record.ID)}
	if !requestPayload {
		keys = []string{
			biz.GenerateResponseBodyKey(record.ProjectID, record.ID),
			biz.GenerateResponseChunksKey(record.ProjectID, record.ID),
		}
	}
	for _, key := range keys {
		if err := w.DataStorageService.DeleteData(ctx, storage, key); err != nil {
			log.Warn(ctx, "Failed to delete external request payload",
				log.Int("request_id", record.ID),
				log.String("key", key),
				log.Cause(err))
		}
	}
}

func (w *Worker) cleanupExecutionPayloadExternalStorage(
	ctx context.Context,
	execution *ent.RequestExecution,
	cache map[int]*ent.DataStorage,
	requestPayload bool,
) {
	if execution == nil || execution.DataStorageID == 0 || w.DataStorageService == nil {
		return
	}

	storage, err := w.getDataStorageCached(ctx, execution.DataStorageID, cache)
	if err != nil {
		log.Warn(ctx, "Failed to load data storage while clearing execution payload",
			log.Int("execution_id", execution.ID),
			log.Cause(err))
		return
	}
	if storage == nil || storage.Primary {
		return
	}

	keys := []string{biz.GenerateExecutionRequestBodyKey(execution.ProjectID, execution.RequestID, execution.ID)}
	if !requestPayload {
		keys = []string{
			biz.GenerateExecutionResponseBodyKey(execution.ProjectID, execution.RequestID, execution.ID),
			biz.GenerateExecutionResponseChunksKey(execution.ProjectID, execution.RequestID, execution.ID),
		}
	}
	for _, key := range keys {
		if err := w.DataStorageService.DeleteData(ctx, storage, key); err != nil {
			log.Warn(ctx, "Failed to delete external execution payload",
				log.Int("execution_id", execution.ID),
				log.String("key", key),
				log.Cause(err))
		}
	}
}
