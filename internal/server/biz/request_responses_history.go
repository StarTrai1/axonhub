package biz

import (
	"context"
	"fmt"
	"time"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/predicate"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
)

// ResponsesHistoryEvidenceScope identifies retained executions from the current
// channel configuration and caller. Suffixes are only used after the caller has
// verified that the selected credential uniquely owns the suffix in this channel.
type ResponsesHistoryEvidenceScope struct {
	ChannelID        int
	APIKeyID         int
	ProjectID        int
	Model            string
	URL              string
	CredentialSuffix string
	Since            time.Time
}

// FindResponsesHistoryEvidence returns bounded metadata, without loading bodies
// or response chunks. A completed sibling alone does not prove compatibility;
// the orchestrator must compare the actual failed and successful request bodies.
func (s *RequestService) FindResponsesHistoryEvidence(ctx context.Context, scope ResponsesHistoryEvidenceScope) ([]*ent.RequestExecution, error) {
	if scope.ChannelID <= 0 || scope.APIKeyID <= 0 || scope.ProjectID <= 0 || scope.Model == "" ||
		scope.URL == "" || scope.CredentialSuffix == "" || scope.Since.IsZero() {
		return nil, nil
	}
	predicates := []predicate.RequestExecution{
		requestexecution.ChannelIDEQ(scope.ChannelID),
		requestexecution.ProjectIDEQ(scope.ProjectID),
		requestexecution.ModelIDEQ(scope.Model),
		requestexecution.RequestURLEQ(scope.URL),
		requestexecution.ChannelAPIKeySuffixEQ(scope.CredentialSuffix),
		requestexecution.CreatedAtGTE(scope.Since),
		requestexecution.FormatEQ(string(llm.APIFormatOpenAIResponse)),
		requestexecution.HasRequestWith(request.APIKeyIDEQ(scope.APIKeyID), request.ProjectIDEQ(scope.ProjectID), request.StatusEQ(request.StatusCompleted)),
	}
	fields := []string{
		requestexecution.FieldID, requestexecution.FieldRequestID, requestexecution.FieldProjectID,
		requestexecution.FieldCreatedAt, requestexecution.FieldStatus, requestexecution.FieldErrorMessage,
		requestexecution.FieldResponseStatusCode, requestexecution.FieldDataStorageID,
	}
	client := s.entFromContext(ctx)
	failed, err := client.RequestExecution.Query().Where(predicates...).
		Where(requestexecution.StatusEQ(requestexecution.StatusFailed), requestexecution.ResponseStatusCodeEQ(400)).
		Order(ent.Desc(requestexecution.FieldID)).Limit(24).Select(fields...).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("find Responses history rejections: %w", err)
	}
	if len(failed) == 0 {
		return nil, nil
	}
	requestIDs := lo.Uniq(lo.Map(failed, func(execution *ent.RequestExecution, _ int) int { return execution.RequestID }))
	completed, err := client.RequestExecution.Query().Where(predicates...).
		Where(requestexecution.RequestIDIn(requestIDs...), requestexecution.StatusEQ(requestexecution.StatusCompleted)).
		Order(ent.Desc(requestexecution.FieldID)).Limit(24).Select(fields...).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("find Responses history corrections: %w", err)
	}
	return append(failed, completed...), nil
}

func (s *RequestService) LoadResponsesHistoryEvidenceBody(ctx context.Context, execution *ent.RequestExecution) (objects.JSONRawMessage, error) {
	stored, err := s.entFromContext(ctx).RequestExecution.Query().
		Where(requestexecution.IDEQ(execution.ID), requestexecution.RequestIDEQ(execution.RequestID), requestexecution.ProjectIDEQ(execution.ProjectID)).
		Select(requestexecution.FieldID, requestexecution.FieldRequestID, requestexecution.FieldProjectID, requestexecution.FieldDataStorageID, requestexecution.FieldRequestBody).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("load Responses history evidence: %w", err)
	}
	return s.LoadRequestExecutionRequestBody(ctx, stored)
}
