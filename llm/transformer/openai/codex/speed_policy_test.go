package codex

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestSpeedPolicyChoosesBothRelayAliases(t *testing.T) {
	for _, tc := range []struct{ name, requested, tier, configured, wantModel, wantTier string }{
		{"fast default", "gpt-6.1-sol", "", "priority", "gpt-6.1-sol-fast", ""},
		{"ultrafast default", "gpt-6.1-sol", "", "ultrafast", "gpt-6.1-sol-ultrafast", ""},
		{"explicit tier", "gpt-6.1-sol", "ultrafast", "priority", "gpt-6.1-sol-ultrafast", ""},
		{"explicit alias", "gpt-6.1-sol-fast", "", "ultrafast", "gpt-6.1-sol-fast", ""},
		{"explicit standard", "gpt-6.1-sol-ultrafast", "default", "priority", "gpt-6.1-sol", "default"},
		{"unchanged default", "gpt-6.1-sol", "", "", "gpt-6.1-sol", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := &OutboundTransformer{defaultServiceTier: tc.configured, speedModels: map[string]bool{"gpt-6.1-sol-fast": true, "gpt-6.1-sol-ultrafast": true}}
			model, tier := out.resolveSpeed(tc.requested, lo.ToPtr(tc.tier))
			require.Equal(t, tc.wantModel, model)
			if tier == nil {
				require.Empty(t, tc.wantTier)
			} else {
				require.Equal(t, tc.wantTier, *tier)
			}
			body := []byte(`{"model":"` + tc.requested + `","service_tier":"` + tc.tier + `","input":[{"type":"compaction","encrypted_content":"keep"}],"prompt_cache_key":"session-keep"}`)
			wire := out.normalizeFastModelRequest(&httpclient.Request{Body: body})
			require.Equal(t, tc.wantModel, gjson.GetBytes(wire.Body, "model").String())
			require.Equal(t, tc.wantTier, gjson.GetBytes(wire.Body, "service_tier").String())
			require.Equal(t, "keep", gjson.GetBytes(wire.Body, "input.0.encrypted_content").String())
			require.Equal(t, "session-keep", gjson.GetBytes(wire.Body, "prompt_cache_key").String())
			require.Equal(t, wire.Body, out.normalizeFastModelRequest(wire).Body)
		})
	}
}

func TestSpeedPolicyNativeTierWithoutRelayAlias(t *testing.T) {
	out := &OutboundTransformer{defaultServiceTier: "ultrafast"}
	model, tier := out.resolveSpeed("gpt-6.1-sol", nil)
	require.Equal(t, "gpt-6.1-sol", model)
	require.Equal(t, "ultrafast", *tier)
}
