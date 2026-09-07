package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
)

func TestHTTP2PeerResetRetryClassification(t *testing.T) {
	for _, scenario := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "internal reset", err: fmt.Errorf("failed to stream request: %w", errors.New("stream error: stream ID 65; INTERNAL_ERROR; received from peer")), want: true},
		{name: "refused stream", err: errors.New("stream error: stream ID 1; REFUSED_STREAM; received from peer"), want: true},
		{name: "local protocol violation", err: errors.New("stream error: stream ID 1; PROTOCOL_ERROR; received from peer")},
		{name: "local reset", err: errors.New("stream error: stream ID 1; INTERNAL_ERROR; local failure")},
		{name: "unrelated message", err: errors.New("provider says INTERNAL_ERROR")},
		{name: "structured bad request", err: &llm.ResponseError{StatusCode: 400, Detail: llm.ErrorDetail{Message: "stream error: stream ID 1; INTERNAL_ERROR; received from peer"}}},
		{name: "canceled", err: errors.Join(context.Canceled, errors.New("stream error: stream ID 1; INTERNAL_ERROR; received from peer"))},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			require.Equal(t, scenario.want, isRetryableTransportError(scenario.err))
			require.Equal(t, scenario.want, IsUpstreamTransportError(scenario.err))
		})
	}
}

func TestHTTP2PeerResetRetriesStickyLastChannel(t *testing.T) {
	candidate := &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: 29}}, TraceSticky: true}
	state := &PersistenceState{CurrentCandidate: candidate, ChannelModelsCandidates: []*ChannelModelsCandidate{candidate}}
	outbound := &PersistentOutboundTransformer{state: state}
	failure := errors.New("stream error: stream ID 65; INTERNAL_ERROR; received from peer")
	require.True(t, outbound.CanRetry(failure))
	state.ChannelModelsCandidates = append(state.ChannelModelsCandidates, &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: 30}}})
	require.False(t, outbound.CanRetry(failure))
}
