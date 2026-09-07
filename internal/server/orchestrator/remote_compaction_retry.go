package orchestrator

import (
	"context"

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
	if state.RetryPolicyProvider != nil {
		policy := state.RetryPolicyProvider.RetryPolicyOrDefault(ctx)
		if policy.Enabled {
			maxRetries = policy.MaxSingleChannelRetries
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
			return stream, execution, nil
		}
		a.markBridgeExecutionFailed(ctx, execution, err)
		compatibility.OnOutboundRawError(ctx, err)
		if attempt >= maxRetries || !hasResponsesRejectedStatusCompatibilityRetry(state, state.CurrentCandidate.Channel.ID) {
			return nil, execution, err
		}
		providerRequest, err = compatibility.OnOutboundRawRequest(ctx, providerRequest)
		if err != nil {
			return nil, execution, err
		}
	}
}
