package pipeline

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
)

type admissionTestOutbound struct {
	mockOutbound
	allow func(error) bool
}

func (o *admissionTestOutbound) AllowRetry(err error) bool { return o.allow(err) }

func TestRetryAdmissionStopsSameAndCrossChannelRecovery(t *testing.T) {
	for _, same := range []bool{true, false} {
		t.Run(map[bool]string{true: "same", false: "cross"}[same], func(t *testing.T) {
			calls := 0
			cause := errors.New("provider content refusal")
			outbound := &admissionTestOutbound{mockOutbound: mockOutbound{
				canRetry:        func(error) bool { return same },
				hasMoreChannels: func() bool { return true },
			}, allow: func(err error) bool { require.ErrorIs(t, err, cause); return false }}
			p := &pipeline{Inbound: &mockInbound{}, Outbound: outbound, Executor: &mockExecutor{do: func(context.Context, *httpclient.Request) (*httpclient.Response, error) { calls++; return nil, cause }}, maxSameChannelRetries: 10, maxChannelRetries: 10}
			_, err := p.Process(t.Context(), &httpclient.Request{})
			require.ErrorIs(t, err, cause)
			require.Equal(t, 1, calls)
		})
	}
}
