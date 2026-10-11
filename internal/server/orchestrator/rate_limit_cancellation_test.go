package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

type canceledQuotaExecutor struct {
	cancel   context.CancelFunc
	frame    string
	failure  error
	direct   bool
	attempts int
}

func (*canceledQuotaExecutor) Do(context.Context, *httpclient.Request) (*httpclient.Response, error) {
	return nil, errors.New("unexpected non-stream request")
}

func (e *canceledQuotaExecutor) DoStream(context.Context, *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	e.attempts++
	if e.direct {
		e.cancel()
		return nil, e.failure
	}
	inner := &errorAfterEventsStream{err: e.failure}
	if e.frame != "" {
		inner.items = []*httpclient.StreamEvent{{Data: []byte(e.frame)}}
	}
	return &cancelQuotaOnReadStream{Stream: inner, cancel: e.cancel}, nil
}

type cancelQuotaOnReadStream struct {
	streams.Stream[*httpclient.StreamEvent]

	cancel context.CancelFunc
}

func (s *cancelQuotaOnReadStream) Next() bool {
	hasNext := s.Stream.Next()
	s.cancel()
	return hasNext
}

func TestCanceledResponsesStreamPreservesQuotaCooldown(t *testing.T) {
	quota := &httpclient.Error{
		StatusCode: http.StatusTooManyRequests,
		Body:       []byte(`{"error":{"type":"usage_limit_reached","resets_in_seconds":600}}`),
	}
	for _, test := range []struct {
		name    string
		frame   string
		failure error
		direct  bool
		cool    bool
	}{
		{name: "before stream", failure: quota, direct: true, cool: true},
		{name: "read error", failure: quota, cool: true},
		{name: "WebSocket error frame", frame: `{"type":"error","status":429,"error":{"type":"usage_limit_reached","message":"quota exhausted","resets_in_seconds":600}}`, cool: true},
		{name: "failed response", frame: `{"type":"response.failed","response":{"id":"resp_quota","status":"failed","error":{"type":"usage_limit_reached","message":"quota exhausted","resets_in_seconds":600}}}`, cool: true},
		{name: "plain client cancellation", failure: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			real, err := responses.NewOutboundTransformer("https://example.invalid", "test-key")
			require.NoError(t, err)
			candidate := &ChannelModelsCandidate{
				Channel: &biz.Channel{Channel: &ent.Channel{
					ID: 1, Type: entchannel.TypeCodex,
					Credentials: objects.ChannelCredentials{APIKeys: []string{"test-key"}},
				}, Outbound: real},
				APIFormat: string(llm.APIFormatOpenAIResponse),
				Models:    []biz.ChannelModelEntry{{RequestModel: "gpt-6.1-sol", ActualModel: "gpt-6.1-sol"}},
			}
			outbound := &PersistentOutboundTransformer{state: &PersistenceState{
				OriginalModel: "gpt-6.1-sol", ChannelModelsCandidates: []*ChannelModelsCandidate{candidate},
			}}
			tracker := NewChannelRequestTracker()
			executor := &canceledQuotaExecutor{cancel: cancel, frame: test.frame, failure: test.failure, direct: test.direct}
			processor := pipeline.NewFactory(executor).Pipeline(openai.NewInboundTransformer(), outbound,
				pipeline.WithRetry(1, 1, 0), pipeline.WithResponseTimeouts(time.Minute, 0),
				pipeline.WithMiddlewares(withRateLimitTracking(outbound, tracker)))
			before := time.Now()
			_, err = processor.Process(ctx, buildTestRequest("gpt-6.1-sol", "hello", true))
			require.Error(t, err)
			require.ErrorIs(t, ctx.Err(), context.Canceled)
			require.Equal(t, 1, executor.attempts, "cancellation must still prevent retries")
			until, cooled := tracker.GetCooldownUntil(1)
			require.Equal(t, test.cool, cooled)
			if test.cool {
				require.WithinDuration(t, before.Add(10*time.Minute), until, time.Second)
			} else {
				require.ErrorIs(t, err, context.Canceled)
			}
		})
	}
}
