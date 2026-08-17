package biz

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/scheduler"
)

const payloadMigrationBatchSize = 8

// PayloadCompressionService incrementally compresses legacy database payloads.
// New payloads are compressed inline, so this worker only drains existing rows.
type PayloadCompressionService struct {
	db *ent.Client

	mu              sync.Mutex
	requestCursor   int
	executionCursor int
}

// NewPayloadCompressionService creates the legacy payload migration worker.
func NewPayloadCompressionService(db *ent.Client) *PayloadCompressionService {
	return &PayloadCompressionService{db: db}
}

func (s *PayloadCompressionService) RegisterScheduledTasks(ctx context.Context, sched *scheduler.Scheduler) error {
	return sched.Register(ctx, scheduler.TaskSpec{
		Name:        "database-payload-compression",
		Description: "Compress legacy request payloads in bounded batches",
		FixRate:     15 * time.Second,
	}, s.run)
}

func (s *PayloadCompressionService) run(ctx context.Context) {
	if !s.mu.TryLock() {
		return
	}
	defer s.mu.Unlock()

	ctx = authz.WithSystemBypass(ctx, "database-payload-compression")
	ctx = ent.NewContext(ctx, s.db)

	if err := s.compressRequests(ctx); err != nil {
		log.Warn(ctx, "Failed to compress legacy request payloads", log.Cause(err))
	}
	if err := s.compressRequestExecutions(ctx); err != nil {
		log.Warn(ctx, "Failed to compress legacy execution payloads", log.Cause(err))
	}
}

func (s *PayloadCompressionService) compressRequests(ctx context.Context) error {
	rows, err := s.db.Request.Query().
		Where(request.IDGT(s.requestCursor)).
		Order(ent.Asc(request.FieldID)).
		Limit(payloadMigrationBatchSize).
		Select(
			request.FieldID,
			request.FieldUpdatedAt,
			request.FieldRequestBody,
			request.FieldResponseBody,
			request.FieldStatus,
		).
		All(ctx)
	if err != nil {
		return fmt.Errorf("query requests: %w", err)
	}

	for _, row := range rows {
		if !isFinishedStreamStatus(row.Status) {
			return nil
		}

		requestBody, requestChanged, err := compressLegacyPayload(row.RequestBody)
		if err != nil {
			return fmt.Errorf("compress request %d body: %w", row.ID, err)
		}
		responseBody, responseChanged, err := compressLegacyPayload(row.ResponseBody)
		if err != nil {
			return fmt.Errorf("compress request %d response: %w", row.ID, err)
		}
		if !requestChanged && !responseChanged {
			s.requestCursor = row.ID
			continue
		}

		update := s.db.Request.UpdateOneID(row.ID).
			Where(request.UpdatedAtEQ(row.UpdatedAt)).
			SetUpdatedAt(row.UpdatedAt)
		if requestChanged {
			update.Modify(func(builder *entsql.UpdateBuilder) {
				builder.Set(request.FieldRequestBody, string(requestBody))
			})
		}
		if responseChanged {
			update.SetResponseBody(responseBody)
		}
		if _, err := update.Save(ctx); err != nil {
			return fmt.Errorf("update request %d: %w", row.ID, err)
		}
		s.requestCursor = row.ID
	}

	return nil
}

func (s *PayloadCompressionService) compressRequestExecutions(ctx context.Context) error {
	rows, err := s.db.RequestExecution.Query().
		Where(requestexecution.IDGT(s.executionCursor)).
		Order(ent.Asc(requestexecution.FieldID)).
		Limit(payloadMigrationBatchSize).
		Select(
			requestexecution.FieldID,
			requestexecution.FieldUpdatedAt,
			requestexecution.FieldRequestBody,
			requestexecution.FieldResponseBody,
			requestexecution.FieldStatus,
		).
		All(ctx)
	if err != nil {
		return fmt.Errorf("query request executions: %w", err)
	}

	for _, row := range rows {
		if !isFinishedExecutionStatus(row.Status) {
			return nil
		}

		requestBody, requestChanged, err := compressLegacyPayload(row.RequestBody)
		if err != nil {
			return fmt.Errorf("compress request execution %d body: %w", row.ID, err)
		}
		responseBody, responseChanged, err := compressLegacyPayload(row.ResponseBody)
		if err != nil {
			return fmt.Errorf("compress request execution %d response: %w", row.ID, err)
		}
		if !requestChanged && !responseChanged {
			s.executionCursor = row.ID
			continue
		}

		update := s.db.RequestExecution.UpdateOneID(row.ID).
			Where(requestexecution.UpdatedAtEQ(row.UpdatedAt)).
			SetUpdatedAt(row.UpdatedAt)
		if requestChanged {
			update.Modify(func(builder *entsql.UpdateBuilder) {
				builder.Set(requestexecution.FieldRequestBody, string(requestBody))
			})
		}
		if responseChanged {
			update.SetResponseBody(responseBody)
		}
		if _, err := update.Save(ctx); err != nil {
			return fmt.Errorf("update request execution %d: %w", row.ID, err)
		}
		s.executionCursor = row.ID
	}

	return nil
}

func compressLegacyPayload(raw []byte) ([]byte, bool, error) {
	compressed, err := CompressStoredPayload(raw)
	if err != nil {
		return nil, false, err
	}

	return compressed, !bytes.Equal(compressed, raw), nil
}
