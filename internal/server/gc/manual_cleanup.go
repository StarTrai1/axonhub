package gc

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"github.com/google/uuid"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channelprobe"
	"github.com/looplj/axonhub/internal/ent/datastorage"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/ent/schema/schematype"
	"github.com/looplj/axonhub/internal/ent/trace"
	"github.com/looplj/axonhub/internal/ent/usagelog"
	"github.com/looplj/axonhub/internal/log"
)

func (w *Worker) PreviewManualCleanup(ctx context.Context, input ManualCleanupInput) ([]CleanupPreviewItem, error) {
	manualDays, _, err := validateManualCleanupInput(input, false)
	if err != nil {
		return nil, err
	}

	ctx = authz.WithSystemBypass(ctx, "storage-cleanup-preview")
	ctx = ent.NewContext(ctx, w.Ent)
	ctx = schematype.SkipSoftDelete(ctx)

	items := make([]CleanupPreviewItem, 0, len(input.Resources))
	for _, selection := range input.Resources {
		cutoff := cleanupCutoff(selection.RetentionDays)
		count, err := w.previewResourceCount(ctx, selection.ResourceType, cutoff)
		if err != nil {
			return nil, err
		}
		bytes, err := w.previewResourceBytes(ctx, selection.ResourceType, cutoff)
		if err != nil {
			return nil, err
		}
		_, sensitive := sensitiveResourceTypes[selection.ResourceType]
		items = append(items, CleanupPreviewItem{
			ResourceType:   selection.ResourceType,
			EstimatedCount: count,
			EstimatedBytes: bytes,
			CutoffTime:     cutoff,
			RetentionDays:  manualDays[selection.ResourceType],
			Sensitive:      sensitive,
		})
	}

	return items, nil
}

func (w *Worker) StartManualCleanup(ctx context.Context, input ManualCleanupInput) (*CleanupJobStatus, error) {
	manualDays, _, err := validateManualCleanupInput(input, true)
	if err != nil {
		return nil, err
	}
	if !w.cleanupMu.TryLock() {
		return nil, ErrCleanupAlreadyRunning
	}

	job := &CleanupJobStatus{
		ID:        uuid.NewString(),
		Status:    "running",
		Phase:     "starting",
		StartedAt: time.Now(),
	}
	w.jobMu.Lock()
	w.currentJob = job
	w.jobMu.Unlock()

	background := authz.WithSystemBypass(context.WithoutCancel(ctx), "manual-storage-cleanup")
	go func() {
		defer w.cleanupMu.Unlock()
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Error(background, "Storage cleanup panicked",
					log.Any("panic", recovered),
					log.String("stack", string(debug.Stack())),
				)
				w.finishCleanupJob(fmt.Errorf("cleanup panicked: %v", recovered))
			}
		}()

		err := w.runCleanupLocked(background, true, manualDays, w.updateCleanupJobPhase)
		w.finishCleanupJob(err)
	}()

	return w.CurrentCleanupJob(), nil
}

func (w *Worker) CurrentCleanupJob() *CleanupJobStatus {
	w.jobMu.RLock()
	defer w.jobMu.RUnlock()

	if w.currentJob == nil {
		return nil
	}
	copy := *w.currentJob
	return &copy
}

func (w *Worker) updateCleanupJobPhase(phase string) {
	w.jobMu.Lock()
	defer w.jobMu.Unlock()
	if w.currentJob != nil && w.currentJob.Status == "running" {
		w.currentJob.Phase = phase
	}
}

func (w *Worker) finishCleanupJob(err error) {
	w.jobMu.Lock()
	defer w.jobMu.Unlock()
	if w.currentJob == nil || w.currentJob.Status != "running" {
		return
	}

	finishedAt := time.Now()
	w.currentJob.FinishedAt = &finishedAt
	if err != nil {
		w.currentJob.Status = "failed"
		w.currentJob.Phase = "failed"
		w.currentJob.Error = err.Error()
		return
	}

	w.currentJob.Status = "completed"
	w.currentJob.Phase = "completed"
}

func (w *Worker) previewResourceCount(ctx context.Context, resourceType string, cutoff time.Time) (int, error) {
	switch resourceType {
	case ResourceRequestPayloads:
		requestsCount, err := w.Ent.Request.Query().Where(
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
		).Count(ctx)
		if err != nil {
			return 0, fmt.Errorf("count request payloads: %w", err)
		}
		executionsCount, err := w.Ent.RequestExecution.Query().Where(
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
		).Count(ctx)
		if err != nil {
			return 0, fmt.Errorf("count execution request payloads: %w", err)
		}
		return requestsCount + executionsCount, nil
	case ResourceResponsePayloads:
		requestsCount, err := w.Ent.Request.Query().Where(
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
		).Count(ctx)
		if err != nil {
			return 0, fmt.Errorf("count request responses: %w", err)
		}
		executionsCount, err := w.Ent.RequestExecution.Query().Where(
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
		).Count(ctx)
		if err != nil {
			return 0, fmt.Errorf("count execution responses: %w", err)
		}
		return requestsCount + executionsCount, nil
	case ResourceRequests:
		requestsCount, err := w.Ent.Request.Query().Where(
			request.CreatedAtLT(cutoff),
			request.StatusNotIn(request.StatusPending, request.StatusProcessing),
			request.Not(request.HasTraceWith(trace.StatusEQ(trace.StatusRetained))),
			request.Not(request.HasExecutionsWith(
				requestexecution.StatusIn(requestexecution.StatusPending, requestexecution.StatusProcessing),
			)),
		).Count(ctx)
		if err != nil {
			return 0, fmt.Errorf("count request records: %w", err)
		}
		executionsCount, err := w.Ent.RequestExecution.Query().Where(
			requestexecution.StatusNotIn(requestexecution.StatusPending, requestexecution.StatusProcessing),
			requestexecution.Or(
				requestexecution.HasRequestWith(
					request.CreatedAtLT(cutoff),
					request.StatusNotIn(request.StatusPending, request.StatusProcessing),
					request.Not(request.HasTraceWith(trace.StatusEQ(trace.StatusRetained))),
					request.Not(request.HasExecutionsWith(
						requestexecution.StatusIn(requestexecution.StatusPending, requestexecution.StatusProcessing),
					)),
				),
				requestexecution.And(
					requestexecution.CreatedAtLT(cutoff),
					requestexecution.Not(requestexecution.HasRequest()),
				),
			),
		).Count(ctx)
		if err != nil {
			return 0, fmt.Errorf("count request executions: %w", err)
		}
		return requestsCount + executionsCount, nil
	case ResourceUsageLogs:
		count, err := w.Ent.UsageLog.Query().Where(
			usagelog.CreatedAtLT(cutoff),
			usagelog.Not(usagelog.HasRequestWith(
				request.HasTraceWith(trace.StatusEQ(trace.StatusRetained)),
			)),
		).Count(ctx)
		if err != nil {
			return 0, fmt.Errorf("count usage logs: %w", err)
		}
		return count, nil
	case ResourceChannelProbes:
		count, err := w.Ent.ChannelProbe.Query().Where(
			channelprobe.TimestampLT(cutoff.Unix()),
		).Count(ctx)
		if err != nil {
			return 0, fmt.Errorf("count channel probes: %w", err)
		}
		return count, nil
	default:
		return 0, fmt.Errorf("unsupported cleanup resource %q", resourceType)
	}
}

func (w *Worker) previewResourceBytes(ctx context.Context, resourceType string, cutoff time.Time) (int64, error) {
	if resourceType != ResourceRequestPayloads &&
		resourceType != ResourceResponsePayloads &&
		resourceType != ResourceRequests {
		return 0, nil
	}

	driver, ok := w.Ent.Driver().(*entsql.Driver)
	if !ok {
		return 0, fmt.Errorf("database driver does not support size estimation")
	}

	placeholder := "?"
	length := func(column string) string {
		return fmt.Sprintf("COALESCE(LENGTH(%s), 0)", column)
	}
	if driver.Dialect() == dialect.Postgres {
		placeholder = "$1"
		length = func(column string) string {
			return fmt.Sprintf("COALESCE(OCTET_LENGTH(%s::text), 0)", column)
		}
	}

	requestColumns := []string{"r.request_body", "r.request_headers"}
	executionColumns := []string{"e.request_body", "e.request_headers"}
	if resourceType == ResourceResponsePayloads {
		requestColumns = []string{"r.response_body", "r.response_chunks"}
		executionColumns = []string{"e.response_body", "e.response_chunks"}
	} else if resourceType == ResourceRequests {
		requestColumns = append(requestColumns, "r.response_body", "r.response_chunks")
		executionColumns = append(executionColumns, "e.response_body", "e.response_chunks")
	}

	sumExpression := func(columns []string) string {
		result := "0"
		for _, column := range columns {
			result += " + " + length(column)
		}
		return result
	}

	requestQuery := fmt.Sprintf(`
		SELECT COALESCE(SUM(%s), 0)
		FROM requests r
		WHERE r.created_at < %s
		  AND r.status NOT IN ('pending', 'processing')
		  AND NOT EXISTS (
		    SELECT 1 FROM traces t WHERE t.id = r.trace_id AND t.status = 'retained'
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM request_executions active
		    WHERE active.request_id = r.id AND active.status IN ('pending', 'processing')
		  )`, sumExpression(requestColumns), placeholder)
	executionTimeColumn := "e.created_at"
	if resourceType == ResourceRequests {
		executionTimeColumn = "r.created_at"
	}
	executionQuery := fmt.Sprintf(`
		SELECT COALESCE(SUM(%s), 0)
		FROM request_executions e
		JOIN requests r ON r.id = e.request_id
		WHERE %s < %s
		  AND e.status NOT IN ('pending', 'processing')
		  AND r.status NOT IN ('pending', 'processing')
		  AND NOT EXISTS (
		    SELECT 1 FROM traces t WHERE t.id = r.trace_id AND t.status = 'retained'
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM request_executions active
		    WHERE active.request_id = r.id AND active.status IN ('pending', 'processing')
		  )`, sumExpression(executionColumns), executionTimeColumn, placeholder)

	var requestBytes int64
	if err := driver.DB().QueryRowContext(ctx, requestQuery, cutoff).Scan(&requestBytes); err != nil {
		return 0, fmt.Errorf("estimate request bytes: %w", err)
	}
	var executionBytes int64
	if err := driver.DB().QueryRowContext(ctx, executionQuery, cutoff).Scan(&executionBytes); err != nil {
		return 0, fmt.Errorf("estimate execution bytes: %w", err)
	}

	return requestBytes + executionBytes, nil
}
