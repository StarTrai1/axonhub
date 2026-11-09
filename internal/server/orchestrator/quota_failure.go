package orchestrator

import (
	"context"
	"errors"

	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/llm"
)

// Quota admission can fail before an upstream execution exists. Preserve its
// local, credential-free error in the parent response so request details do not
// display an unexplained failure. Never store raw provider errors here.
func persistQuotaAdmissionFailure(ctx context.Context, state *PersistenceState, failure error) error {
	if !isQuotaReserveError(failure) || state == nil || state.Request == nil || state.RequestExec != nil {
		return nil
	}
	response, ok := errors.AsType[*llm.ResponseError](failure)
	if !ok {
		return nil
	}
	return state.RequestService.UpdateRequestFinalized(ctx, state.Request, request.StatusFailed, "", map[string]any{
		"error":       response.Detail,
		"status_code": response.StatusCode,
	}, requestLatencyMetrics(state, nil))
}
