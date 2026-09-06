package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/streams"
)

type metadataThenSilenceStream struct {
	ctx     context.Context
	started bool
	closed  bool
}

func (s *metadataThenSilenceStream) Next() bool {
	if !s.started {
		s.started = true
		return true
	}
	<-s.ctx.Done()
	return false
}

func (s *metadataThenSilenceStream) Current() *llm.Response {
	return &llm.Response{ID: "response_created"}
}

func (s *metadataThenSilenceStream) Err() error { return s.ctx.Err() }
func (s *metadataThenSilenceStream) Close() error {
	s.closed = true
	return nil
}

func TestFirstOutputTimeoutRemainsArmedAfterMetadata(t *testing.T) {
	for _, retryBudget := range []int{0, 1} {
		ctx, guard := newFirstEventTimeoutGuard(t.Context(), 25*time.Millisecond)
		defer guard.cancelStream()
		stream := &metadataThenSilenceStream{ctx: ctx}
		processor := &pipeline{maxSameChannelRetries: retryBudget}
		_, err := processor.preReadLlmStream(ctx, stream, guard)
		require.ErrorIs(t, err, ErrStreamFirstEventTimeout)
		require.True(t, stream.closed)
	}
}

func TestFirstOutputStopsTimeoutWithoutDroppingPreamble(t *testing.T) {
	ctx, guard := newFirstEventTimeoutGuard(t.Context(), time.Second)
	defer guard.cancelStream()
	events := []*llm.Response{
		{ID: "created"},
		{Choices: []llm.Choice{{Delta: &llm.Message{Role: "assistant"}}}},
		{Choices: []llm.Choice{{Delta: &llm.Message{ReasoningContent: lo.ToPtr("thinking")}}}},
	}
	processor := new(pipeline)
	stream, err := processor.preReadLlmStream(ctx, streams.SliceStream(events), guard)
	require.NoError(t, err)
	require.Equal(t, firstEventCompleted, guard.state.Load())
	for _, event := range events {
		require.True(t, stream.Next())
		require.Equal(t, event, stream.Current())
	}
	require.NoError(t, ctx.Err())
	require.NoError(t, stream.Close())
}

func TestFirstOutputTimeoutAllowsTerminalWithoutContent(t *testing.T) {
	ctx, guard := newFirstEventTimeoutGuard(t.Context(), time.Second)
	defer guard.cancelStream()
	processor := new(pipeline)
	stream, err := processor.preReadLlmStream(ctx, streams.SliceStream([]*llm.Response{llm.DoneResponse}), guard)
	require.NoError(t, err)
	require.True(t, stream.Next())
	require.Equal(t, llm.DoneResponse, stream.Current())
	require.Equal(t, firstEventCompleted, guard.state.Load())
	require.NoError(t, stream.Close())
}
