package orchestrator

import (
	"context"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

// Restore a lost native connection from completed, owner-scoped wire records.
// Original archives can be much larger and contain foreign private state;
// replay preserves the last accepted checkpoint instead of expanding it.
func (p *PersistentOutboundTransformer) RestoreResponsesContinuation(ctx context.Context, request *llm.Request) (*llm.Request, error) {
	state := p.state
	if state == nil || request == nil {
		return nil, nil
	}
	c := state.ResponsesContinuation
	format := ""
	if state.CurrentCandidate != nil {
		format = state.CurrentCandidate.APIFormat
	}
	if state.RawProviderRequest != nil {
		format = state.RawProviderRequest.APIFormat
	}
	if state.SystemService == nil || !state.SystemService.UnifiedHistory.Enabled || state.RequestService == nil ||
		c == nil || c.previous == "" || c.complete || state.RawRequest == nil ||
		format != string(llm.APIFormatOpenAIResponseWebSocket) {
		return nil, nil
	}
	adapter := &remoteCompactionAdapter{requestService: state.RequestService, systemService: state.SystemService}
	source, err := adapter.resolveRetainedContinuation(ctx, &remoteCompactionSource{body: c.original, headers: state.RawRequest.Headers}, state, true)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		log.Warn(ctx, "could not restore native continuation from completed wire history", log.Cause(err))
		return nil, nil
	}
	if source == nil || gjson.GetBytes(source.body, "previous_response_id").String() != "" {
		return nil, nil
	}
	body, err := protectUnifiedHistory(ctx, source.body, state)
	if err != nil {
		return nil, err
	}
	raw := cloneRawRequest(state.RawRequest)
	raw.Body = body
	if len(raw.JSONBody) > 0 {
		raw.JSONBody = body
	}
	parsed, err := responses.NewInboundTransformer().TransformRequest(ctx, raw)
	if err != nil {
		return nil, err
	}
	restored := *request
	restored.Messages, restored.ProviderExtensions, restored.RawRequest = parsed.Messages, parsed.ProviderExtensions, raw
	// The pipeline's one-shot recovery keeps this channel/model without invoking
	// model failover or publishing an intermediate foreground failure.
	c.original, c.previous, c.owner = body, "", nil
	state.RawRequest, state.LlmRequest = raw, &restored
	state.RequestExec = nil
	state.PassThroughApplied, state.CleanEOFCompletionEvidence, state.StreamCompleted = false, false, false
	state.OutboundStreamTerminal = streamTerminalNone
	p.resetPassThroughStreamState()
	log.Info(ctx, "restored lost native continuation from completed wire history", log.Int("input_items", int(gjson.GetBytes(body, "input.#").Int())))
	return &restored, nil
}
