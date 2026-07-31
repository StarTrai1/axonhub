package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
)

func TestLoadBalancedSelector_ManualSwitchRetainsDistinctChannel(t *testing.T) {
	firstChannel := &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "first"}}
	secondChannel := &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "second"}}
	candidates := []*ChannelModelsCandidate{
		{Channel: firstChannel, Priority: 0, Models: []biz.ChannelModelEntry{{ActualModel: "model-a"}}},
		{Channel: firstChannel, Priority: 1, Models: []biz.ChannelModelEntry{{ActualModel: "model-b"}}},
		{Channel: secondChannel, Priority: 2, Models: []biz.ChannelModelEntry{{ActualModel: "model-c"}}},
	}
	policy := &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: false}}
	selector := WithLoadBalancedSelector(
		&staticChannelSelector{candidates: candidates},
		&LoadBalancer{},
		policy,
	).WithManualSwitchCandidate()

	result, err := selector.Select(context.Background(), &llm.Request{Model: "model"})
	require.NoError(t, err)
	require.Len(t, result, 3)
	require.Equal(t, firstChannel.ID, result[0].Channel.ID)
	require.Equal(t, secondChannel.ID, result[2].Channel.ID)
}
