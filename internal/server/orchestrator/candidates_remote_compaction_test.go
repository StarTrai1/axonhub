package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestRemoteCompactionSelectorSelect(t *testing.T) {
	newCandidate := func(name string, typ channel.Type, policy objects.RemoteCompactionPolicy) *ChannelModelsCandidate {
		return &ChannelModelsCandidate{
			Models: []biz.ChannelModelEntry{{RequestModel: name}},
			Channel: &biz.Channel{Channel: &ent.Channel{
				Type: typ,
				Policies: objects.ChannelPolicies{
					RemoteCompaction: policy,
				},
			}},
		}
	}
	newLegacyCandidate := func(name string, supportsRemoteCompaction bool) *ChannelModelsCandidate {
		candidate := newCandidate(name, channel.TypeCodex, "")
		candidate.Channel.Policies.SupportsRemoteCompaction = supportsRemoteCompaction

		return candidate
	}

	modelNames := func(candidates []*ChannelModelsCandidate) []string {
		names := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			names = append(names, candidate.Models[0].RequestModel)
		}
		return names
	}

	legacyRequest := &llm.Request{
		RequestType: llm.RequestTypeCompact,
		APIFormat:   llm.APIFormatOpenAIResponseCompact,
	}
	v2Request, err := responses.NewInboundTransformer().TransformRequest(context.Background(), &httpclient.Request{
		Body: []byte(`{
			"model":"gpt-5.6-sol",
			"input":[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"compact"}]},
				{"type":"compaction_trigger"}
			],
			"stream":true
		}`),
	})
	require.NoError(t, err)
	continuationRequest, err := responses.NewInboundTransformer().TransformRequest(context.Background(), &httpclient.Request{
		Body: []byte(`{
			"model":"gpt-5.6-sol",
			"input":[
				{"type":"message","role":"user","content":[{"type":"input_text","text":"before compact"}]},
				{"id":"cmp_001","type":"compaction","encrypted_content":"opaque"},
				{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
			],
			"stream":true
		}`),
	})
	require.NoError(t, err)
	normalRequest := &llm.Request{
		RequestType: llm.RequestTypeChat,
		APIFormat:   llm.APIFormatOpenAIResponse,
		RawRequest: &httpclient.Request{Headers: http.Header{
			"X-Codex-Beta-Features": []string{"remote_compaction_v2"},
		}},
	}

	tests := []struct {
		name       string
		request    *llm.Request
		candidates []*ChannelModelsCandidate
		err        error
		want       []string
		wantErr    bool
	}{
		{
			name:    "legacy compact prefers capable Codex channels",
			request: legacyRequest,
			candidates: []*ChannelModelsCandidate{
				newCandidate("automatic", channel.TypeCodex, objects.RemoteCompactionPolicyAuto),
				newCandidate("capable-a", channel.TypeCodex, objects.RemoteCompactionPolicyNative),
				newCandidate("capable-b", channel.TypeCodex, objects.RemoteCompactionPolicyNative),
			},
			want: []string{"capable-a", "capable-b"},
		},
		{
			name:    "v2 trigger prefers capable Codex channels",
			request: v2Request,
			candidates: []*ChannelModelsCandidate{
				newCandidate("automatic", channel.TypeCodex, objects.RemoteCompactionPolicyAuto),
				newCandidate("capable", channel.TypeCodex, objects.RemoteCompactionPolicyNative),
			},
			want: []string{"capable"},
		},
		{
			name:    "post-compaction turn prefers capable Codex channels",
			request: continuationRequest,
			candidates: []*ChannelModelsCandidate{
				newCandidate("local", channel.TypeCodex, objects.RemoteCompactionPolicyLocalBridge),
				newCandidate("capable", channel.TypeCodex, objects.RemoteCompactionPolicyNative),
			},
			want: []string{"capable"},
		},
		{
			name:    "normal responses request keeps all candidates",
			request: normalRequest,
			candidates: []*ChannelModelsCandidate{
				newCandidate("local", channel.TypeCodex, objects.RemoteCompactionPolicyLocalBridge),
				newCandidate("capable", channel.TypeCodex, objects.RemoteCompactionPolicyNative),
			},
			want: []string{"local", "capable"},
		},
		{
			name:    "generation prefers explicit local bridge before automatic fallback",
			request: v2Request,
			candidates: []*ChannelModelsCandidate{
				newCandidate("automatic-a", channel.TypeCodex, objects.RemoteCompactionPolicyAuto),
				newCandidate("local", channel.TypeCodex, objects.RemoteCompactionPolicyLocalBridge),
				newCandidate("automatic-b", channel.TypeCodex, objects.RemoteCompactionPolicyAuto),
			},
			want: []string{"local"},
		},
		{
			name:    "continuation fallback keeps automatic and local bridge candidates",
			request: continuationRequest,
			candidates: []*ChannelModelsCandidate{
				newCandidate("automatic", channel.TypeCodex, objects.RemoteCompactionPolicyAuto),
				newCandidate("local", channel.TypeCodex, objects.RemoteCompactionPolicyLocalBridge),
			},
			want: []string{"automatic", "local"},
		},
		{
			name:    "generation with only local bridge remains eligible",
			request: legacyRequest,
			candidates: []*ChannelModelsCandidate{
				newCandidate("local", channel.TypeCodex, objects.RemoteCompactionPolicyLocalBridge),
			},
			want: []string{"local"},
		},
		{
			name:    "generation without native or local bridge keeps automatic fallback",
			request: v2Request,
			candidates: []*ChannelModelsCandidate{
				newCandidate("automatic-a", channel.TypeCodex, objects.RemoteCompactionPolicyAuto),
				newCandidate("automatic-b", channel.TypeCodex, objects.RemoteCompactionPolicyAuto),
			},
			want: []string{"automatic-a", "automatic-b"},
		},
		{
			name:    "legacy false remains an automatic fallback",
			request: v2Request,
			candidates: []*ChannelModelsCandidate{
				newLegacyCandidate("legacy-automatic", false),
			},
			want: []string{"legacy-automatic"},
		},
		{
			name:    "legacy true remains native capable",
			request: v2Request,
			candidates: []*ChannelModelsCandidate{
				newLegacyCandidate("legacy-native", true),
				newCandidate("automatic", channel.TypeCodex, objects.RemoteCompactionPolicyAuto),
			},
			want: []string{"legacy-native"},
		},
		{
			name:    "capability flag is scoped to Codex channels",
			request: v2Request,
			candidates: []*ChannelModelsCandidate{
				newCandidate("openai", channel.TypeOpenaiResponses, objects.RemoteCompactionPolicyNative),
				newCandidate("codex", channel.TypeCodex, objects.RemoteCompactionPolicyNative),
			},
			want: []string{"codex"},
		},
		{
			name:       "empty candidates remain empty",
			request:    v2Request,
			candidates: nil,
			want:       []string{},
		},
		{
			name:    "wrapped selector error is preserved",
			request: v2Request,
			err:     errors.New("wrapped error"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selector := WithRemoteCompactionSelector(&mockSelector{candidates: tt.candidates, err: tt.err})
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
