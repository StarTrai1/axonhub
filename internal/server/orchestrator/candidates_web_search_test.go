package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
)

func TestWebSearchSelector_Select(t *testing.T) {
	newCandidate := func(name string, channelType channel.Type, supported *bool) *ChannelModelsCandidate {
		return &ChannelModelsCandidate{
			Models: []biz.ChannelModelEntry{{RequestModel: name}},
			Channel: &biz.Channel{
				Channel: &ent.Channel{
					Name: name,
					Type: channelType,
					Policies: objects.ChannelPolicies{
						SupportsWebSearch: supported,
					},
				},
			},
		}
	}
	withPolicy := func(candidate *ChannelModelsCandidate, policy objects.WebSearchPolicy) *ChannelModelsCandidate {
		candidate.Channel.Policies.WebSearch = policy
		return candidate
	}
	modelNames := func(candidates []*ChannelModelsCandidate) []string {
		return lo.Map(candidates, func(candidate *ChannelModelsCandidate, _ int) string {
			return candidate.Models[0].RequestModel
		})
	}

	searchRequest := &llm.Request{APIFormat: llm.APIFormatOpenAISearch}
	responsesRequest := &llm.Request{APIFormat: llm.APIFormatOpenAIResponse}
	tests := []struct {
		name       string
		request    *llm.Request
		candidates []*ChannelModelsCandidate
		err        error
		want       []string
		wantErr    bool
	}{
		{
			name:    "explicitly capable Codex channel is preferred",
			request: searchRequest,
			candidates: []*ChannelModelsCandidate{
				newCandidate("disabled", channel.TypeCodex, lo.ToPtr(false)),
				newCandidate("enabled", channel.TypeCodex, lo.ToPtr(true)),
			},
			want: []string{"enabled"},
		},
		{
			name:    "legacy Codex channel defaults to capable",
			request: searchRequest,
			candidates: []*ChannelModelsCandidate{
				newCandidate("disabled", channel.TypeCodex, lo.ToPtr(false)),
				newCandidate("legacy", channel.TypeCodex, nil),
			},
			want: []string{"legacy"},
		},
		{
			name:    "multiple capable channels preserve candidate order",
			request: searchRequest,
			candidates: []*ChannelModelsCandidate{
				newCandidate("first", channel.TypeCodex, lo.ToPtr(true)),
				newCandidate("disabled", channel.TypeCodex, lo.ToPtr(false)),
				newCandidate("second", channel.TypeCodex, nil),
			},
			want: []string{"first", "second"},
		},
		{
			name:    "all disabled falls back to all eligible candidates",
			request: searchRequest,
			candidates: []*ChannelModelsCandidate{
				newCandidate("first", channel.TypeCodex, lo.ToPtr(false)),
				newCandidate("second", channel.TypeCodex, lo.ToPtr(false)),
			},
			want: []string{"first", "second"},
		},
		{
			name:    "explicit native policy is preferred over auto",
			request: searchRequest,
			candidates: []*ChannelModelsCandidate{
				withPolicy(newCandidate("auto", channel.TypeCodex, lo.ToPtr(true)), objects.WebSearchPolicyAuto),
				withPolicy(newCandidate("native", channel.TypeCodex, lo.ToPtr(false)), objects.WebSearchPolicyNative),
			},
			want: []string{"native"},
		},
		{
			name:    "MCP-only channels are excluded from auto fallback",
			request: searchRequest,
			candidates: []*ChannelModelsCandidate{
				withPolicy(newCandidate("mcp-only", channel.TypeCodex, nil), objects.WebSearchPolicyMCPOnly),
				withPolicy(newCandidate("auto", channel.TypeCodex, nil), objects.WebSearchPolicyAuto),
			},
			want: []string{"auto"},
		},
		{
			name:    "all MCP-only channels fail closed",
			request: searchRequest,
			candidates: []*ChannelModelsCandidate{
				withPolicy(newCandidate("first", channel.TypeCodex, nil), objects.WebSearchPolicyMCPOnly),
				withPolicy(newCandidate("second", channel.TypeCodex, nil), objects.WebSearchPolicyMCPOnly),
			},
			want: []string{},
		},
		{
			name:    "capability is scoped to Codex channels",
			request: searchRequest,
			candidates: []*ChannelModelsCandidate{
				newCandidate("openai", channel.TypeOpenaiResponses, lo.ToPtr(true)),
				newCandidate("codex", channel.TypeCodex, lo.ToPtr(true)),
			},
			want: []string{"codex"},
		},
		{
			name:    "non-search request is unchanged",
			request: responsesRequest,
			candidates: []*ChannelModelsCandidate{
				newCandidate("disabled", channel.TypeCodex, lo.ToPtr(false)),
				newCandidate("enabled", channel.TypeCodex, lo.ToPtr(true)),
			},
			want: []string{"disabled", "enabled"},
		},
		{
			name:       "empty candidates remain empty",
			request:    searchRequest,
			candidates: nil,
			want:       []string{},
		},
		{
			name:    "wrapped selector error is preserved",
			request: searchRequest,
			err:     errors.New("wrapped error"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selector := WithWebSearchSelector(&mockSelector{candidates: tt.candidates, err: tt.err})
			got, err := selector.Select(context.Background(), tt.request)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.want, modelNames(got))
		})
	}
}
