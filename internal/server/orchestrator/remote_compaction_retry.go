package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
)

func (a *remoteCompactionAdapter) startLocalCompactionStream(
	ctx context.Context,
	outbound *PersistentOutboundTransformer,
	providerRequest *httpclient.Request,
	executor pipeline.Executor,
	compatibility pipeline.Middleware,
) (streams.Stream[*httpclient.StreamEvent], *ent.RequestExecution, error) {
	state := outbound.state
	maxRetries := 0
	var retryDelay time.Duration
	if state.RetryPolicyProvider != nil {
		policy := state.RetryPolicyProvider.RetryPolicyOrDefault(ctx)
		if policy.Enabled {
			maxRetries = policy.MaxSingleChannelRetries
			retryDelay = time.Duration(policy.RetryDelayMs) * time.Millisecond
		}
	}

	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		state.RawProviderRequest = providerRequest
		state.responsesRejectedStatusRetryChannel = 0
		var execution *ent.RequestExecution
		if state.Request != nil && a.requestService != nil {
			candidate := state.CurrentCandidate
			var err error
			execution, err = a.requestService.CreateRequestExecution(
				ctx,
				candidate.Channel,
				candidate.Models[0].ActualModel,
				state.Request,
				*providerRequest,
				llm.APIFormatOpenAIResponse,
				state.PassThroughApplied,
			)
			if err != nil {
				return nil, nil, err
			}
			state.RequestExec = execution
		}
		stream, err := executor.DoStream(ctx, providerRequest)
		if err == nil {
			observed, observeErr := compatibility.OnOutboundRawStream(ctx, stream)
			if observeErr != nil {
				_ = stream.Close()
				a.markBridgeExecutionFailed(ctx, execution, observeErr)
				return nil, execution, observeErr
			}
			return observed, execution, nil
		}
		err = normalizeLocalCompactionError(ctx, outbound, err)
		a.markBridgeExecutionFailed(ctx, execution, err)
		compatibility.OnOutboundRawError(ctx, err)
		if attempt >= maxRetries || ctx.Err() != nil || !canRetryLocalCompactionStream(outbound, err) {
			return nil, execution, err
		}
		delay := max(retryDelay, outbound.SameChannelRetryDelay(err, attempt+1))
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, execution, ctx.Err()
			case <-timer.C:
			}
		}
		providerRequest, err = compatibility.OnOutboundRawRequest(ctx, providerRequest)
		if err != nil {
			return nil, execution, err
		}
	}
}

func normalizeLocalCompactionError(ctx context.Context, outbound *PersistentOutboundTransformer, err error) error {
	err = ClassifyUpstreamTransportError(err)
	if _, normalized := errors.AsType[*llm.ResponseError](err); normalized {
		return err
	}
	if raw, ok := errors.AsType[*httpclient.Error](err); ok {
		if responseErr := outbound.TransformError(ctx, raw); responseErr != nil {
			if responseErr.Detail.Message == "" {
				responseErr.Detail.Message = ExtractErrorMessage(err)
			}
			return responseErr
		}
	}
	return err
}

// Only failures while opening the stream can retry here. Once a stream is
// returned, its partial summary and terminal outcome belong to that attempt.
func canRetryLocalCompactionStream(outbound *PersistentOutboundTransformer, err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, errSkipCandidateByCircuitBreaker) || isChannelQueueError(err) || isLocalRPMExhaustedError(err) {
		return false
	}
	state := outbound.state
	channel := state.CurrentCandidate.Channel
	if hasResponsesRejectedStatusCompatibilityRetry(state, channel.ID) {
		return true
	}
	status := ExtractStatusCodeFromError(err)
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		return canRetryTransientRateLimit(err)
	}
	// The bridge keeps the selected model. Do not use CanRetry's model-advance
	// branch without rebuilding the provider request for that other model.
	return isRetryableErrorForChannel(err, channel)
}
