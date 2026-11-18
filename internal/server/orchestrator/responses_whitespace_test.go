package orchestrator

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

type whitespaceRelayExecutor struct {
	responsesReasoningPipelineExecutor
	alwaysBlank bool
}

func (e *whitespaceRelayExecutor) DoStream(_ context.Context, request *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	if err := e.capture(request); err != nil {
		return nil, err
	}
	text := " \n"
	if len(e.requests) > 1 && !e.alwaysBlank {
		text = "  continued\n"
	}
	id := fmt.Sprintf("resp_attempt_%d", len(e.requests))
	return streams.SliceStream([]*httpclient.StreamEvent{
		{Type: "response.created", Data: fmt.Appendf(nil, `{"type":"response.created","response":{"id":%q,"model":"gpt-6-astra","status":"in_progress","output":[]}}`, id)},
		{Type: "response.output_text.delta", Data: fmt.Appendf(nil, `{"type":"response.output_text.delta","output_index":0,"content_index":0,"item_id":"msg_synthetic","delta":%q}`, text)},
		{Type: "response.completed", Data: fmt.Appendf(nil, `{"type":"response.completed","response":{"id":%q,"model":"gpt-6-astra","status":"completed","output":[{"type":"message","id":"msg_synthetic","role":"assistant","content":[{"type":"output_text","text":%q}]}]}}`, id, text)},
	}), nil
}

func TestResponsesWhitespaceRelayRetryPreservesDeliveryBoundary(t *testing.T) {
	for _, raw := range []bool{false, true} {
		for _, exhausted := range []bool{false, true} {
			t.Run(fmt.Sprintf("raw=%t/exhausted=%t", raw, exhausted), func(t *testing.T) {
				executor := &whitespaceRelayExecutor{alwaysBlank: exhausted}
				_, result, err := runRejectedReasoningPipeline(t, t.Context(), rejectedReasoningPipelineRequest(t, llm.APIFormatOpenAIResponse), executor, "synthetic-whitespace", raw, 1)
				require.Len(t, executor.requests, 2, "the configured retry budget must bound blank completions")
				if exhausted {
					require.ErrorIs(t, err, llm.ErrEmptyResponse)
					return
				}
				require.NoError(t, err)
				events := drainRejectedReasoningPipeline(t, result)
				for _, event := range events {
					require.NotContains(t, event, "resp_attempt_1", "failed attempt must remain private")
				}
				require.Contains(t, events["response.output_text.delta"], `  continued\n`, "leading and trailing whitespace must survive a meaningful answer")
			})
		}
	}
}
