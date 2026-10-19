package biz

import (
	"context"
	"time"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/llm"
)

// FindUnifiedViewCandidates is a bounded upgrade path for successful views
// recorded before durable view aliases existed. It excludes responses/chunks
// and binds both the execution and its parent to the authenticated project.
func (s *RequestService) FindUnifiedViewCandidates(ctx context.Context, channelID int, since time.Time) ([]*ent.RequestExecution, error) {
	owner, ok := contexts.GetAPIKey(ctx)
	project, found := contexts.GetProjectID(ctx)
	if !ok || !found || owner == nil || owner.ProjectID != project {
		return nil, nil
	}
	return s.entFromContext(ctx).RequestExecution.Query().Where(
		requestexecution.ChannelIDEQ(channelID), requestexecution.ProjectIDEQ(project),
		requestexecution.StatusEQ(requestexecution.StatusCompleted), requestexecution.CreatedAtGTE(since),
		requestexecution.HasRequestWith(request.APIKeyIDEQ(owner.ID), request.ProjectIDEQ(project), request.StatusEQ(request.StatusCompleted),
			request.FormatIn(llm.APIFormatOpenAIResponse.String(), llm.APIFormatOpenAIResponseWebSocket.String())),
	).Select(requestexecution.FieldID, requestexecution.FieldProjectID, requestexecution.FieldRequestID,
		requestexecution.FieldDataStorageID, requestexecution.FieldRequestBody, requestexecution.FieldRequestHeaders,
		requestexecution.FieldRequestURL, requestexecution.FieldChannelAPIKeySuffix).
		WithRequest(func(q *ent.RequestQuery) {
			q.Select(request.FieldID, request.FieldProjectID, request.FieldDataStorageID, request.FieldRequestBody, request.FieldRequestHeaders)
		}).Order(ent.Desc(requestexecution.FieldID)).Limit(128).All(ctx)
}
