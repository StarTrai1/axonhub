package objects

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestChannelPolicies_EffectiveWebSearchPolicy(t *testing.T) {
	tests := []struct {
		name   string
		policy ChannelPolicies
		want   WebSearchPolicy
	}{
		{
			name: "legacy absent defaults to native",
			want: WebSearchPolicyNative,
		},
		{
			name:   "legacy true remains native",
			policy: ChannelPolicies{SupportsWebSearch: lo.ToPtr(true)},
			want:   WebSearchPolicyNative,
		},
		{
			name:   "legacy false becomes automatic fallback",
			policy: ChannelPolicies{SupportsWebSearch: lo.ToPtr(false)},
			want:   WebSearchPolicyAuto,
		},
		{
			name: "explicit policy wins over legacy field",
			policy: ChannelPolicies{
				WebSearch:         WebSearchPolicyMCPOnly,
				SupportsWebSearch: lo.ToPtr(true),
			},
			want: WebSearchPolicyMCPOnly,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.policy.EffectiveWebSearchPolicy())
		})
	}
}

func TestChannelPolicies_EffectiveRemoteCompactionPolicy(t *testing.T) {
	tests := []struct {
		name   string
		policy ChannelPolicies
		want   RemoteCompactionPolicy
	}{
		{
			name: "legacy absent defaults to automatic fallback",
			want: RemoteCompactionPolicyAuto,
		},
		{
			name:   "legacy true remains native",
			policy: ChannelPolicies{SupportsRemoteCompaction: true},
			want:   RemoteCompactionPolicyNative,
		},
		{
			name: "explicit policy wins over legacy field",
			policy: ChannelPolicies{
				RemoteCompaction:         RemoteCompactionPolicyLocalBridge,
				SupportsRemoteCompaction: true,
			},
			want: RemoteCompactionPolicyLocalBridge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.policy.EffectiveRemoteCompactionPolicy())
		})
	}
}
