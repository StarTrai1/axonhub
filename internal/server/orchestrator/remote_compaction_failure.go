package orchestrator

import (
	"context"
	"errors"

	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/objects"
)

type remoteCompactionPreparationError struct {
	cause error
}

func (err *remoteCompactionPreparationError) Error() string { return err.cause.Error() }

func (err *remoteCompactionPreparationError) Unwrap() error { return err.cause }

func persistRemoteCompactionPreparationFailure(ctx context.Context, inbound *PersistentInboundTransformer, failure error) error {
	if _, matched := errors.AsType[*remoteCompactionPreparationError](failure); !matched {
		return nil
	}
	state := inbound.state
	if state.Request != nil || state.APIKey == nil || state.LlmRequest == nil || state.RawRequest == nil {
		return nil
	}
	record, err := state.RequestService.CreateRequest(ctx, state.LlmRequest, state.RawRequest, persistedRequestAPIFormat(ctx, state.LlmRequest.APIFormat))
	if err != nil {
		return err
	}
	state.Request = record
	transformed := inbound.TransformError(ctx, failure)
	if transformed == nil {
		return state.RequestService.UpdateRequestStatusFromError(ctx, record.ID, failure)
	}
	status := request.StatusFailed
	if errors.Is(failure, context.Canceled) {
		status = request.StatusCanceled
	}
	return state.RequestService.UpdateRequestStatusExternalIDAndResponseBody(ctx, record.ID, status, "", objects.JSONRawMessage(transformed.Body), nil)
}
