package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestResponsesRecoveryStopsBothRetryBranches(t *testing.T) {
	// Given
	retries, switches := 0, 0
	outbound := &mockOutbound{
		transformRequest: func(context.Context, *llm.Request) (*httpclient.Request, error) {
			return nil, shared.ResponsesRecoveryError()
		},
		canRetry:        func(error) bool { return true },
		prepareForRetry: func(context.Context) error { retries++; return nil },
		hasMoreChannels: func() bool { return true },
		nextChannel:     func(context.Context) error { switches++; return nil },
	}
	p := &pipeline{Inbound: &mockInbound{}, Outbound: outbound, maxChannelRetries: 2, maxSameChannelRetries: 2}
	// When
	_, err := p.Process(t.Context(), &httpclient.Request{})
	// Then
	require.ErrorIs(t, err, shared.ErrResponsesContinuationUnavailable)
	require.Zero(t, retries)
	require.Zero(t, switches)
}

type restoringOutbound struct {
	*mockOutbound
	restore func(context.Context, *llm.Request) (*llm.Request, error)
}

func (p *restoringOutbound) RestoreResponsesContinuation(ctx context.Context, request *llm.Request) (*llm.Request, error) {
	return p.restore(ctx, request)
}

func TestResponsesRecoveryRestoresOnlyOnceBeforeOrdinaryRetries(t *testing.T) {
	for _, mode := range []string{"repeated rejection", "missing source", "restore error"} {
		t.Run(mode, func(t *testing.T) {
			attempts, restores, retries, switches := 0, 0, 0, 0
			outbound := &restoringOutbound{mockOutbound: &mockOutbound{
				transformRequest: func(context.Context, *llm.Request) (*httpclient.Request, error) {
					attempts++
					return nil, shared.ResponsesRecoveryError()
				},
				canRetry:        func(error) bool { return true },
				prepareForRetry: func(context.Context) error { retries++; return nil },
				hasMoreChannels: func() bool { return true },
				nextChannel:     func(context.Context) error { switches++; return nil },
			}, restore: func(_ context.Context, request *llm.Request) (*llm.Request, error) {
				restores++
				if mode == "restore error" {
					return nil, errors.New("authenticated source rejected")
				}
				if mode == "missing source" {
					return nil, nil
				}
				return request, nil
			}}
			p := &pipeline{Inbound: &mockInbound{}, Outbound: outbound, maxChannelRetries: 3, maxSameChannelRetries: 3}
			_, err := p.Process(t.Context(), &httpclient.Request{})
			require.Error(t, err)
			require.Equal(t, 1, restores)
			want := 1
			if mode == "repeated rejection" {
				want = 2
			}
			require.Equal(t, want, attempts)
			require.Zero(t, retries)
			require.Zero(t, switches)
		})
	}
}
