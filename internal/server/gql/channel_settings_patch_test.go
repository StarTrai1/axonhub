package gql

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/objects"
)

func TestChannelSettingsPatchGraphQLPreservesForkFields(t *testing.T) {
	patch, err := (&executionContext{}).unmarshalInputUpdateChannelSettingsInput(t.Context(), map[string]any{
		"codexSearchOnly": false, "codexSearchModel": "", "httpProtocol": "http1",
		"http2ConnectionShards": 0, "maxSingleChannelRetries": 0,
	})
	require.NoError(t, err)
	merged := patch.Apply(&objects.ChannelSettings{
		CodexSearchOnly: true, CodexSearchModel: "search",
		HTTPProtocol: "auto", HTTP2ConnectionShards: 3, MaxSingleChannelRetries: lo.ToPtr(5), ExtraModelPrefix: "keep",
	})
	require.False(t, merged.CodexSearchOnly)
	require.Empty(t, merged.CodexSearchModel)
	require.Equal(t, "http1", merged.HTTPProtocol)
	require.Zero(t, merged.HTTP2ConnectionShards)
	require.Equal(t, lo.ToPtr(0), merged.MaxSingleChannelRetries)
	require.Equal(t, "keep", merged.ExtraModelPrefix)
}
