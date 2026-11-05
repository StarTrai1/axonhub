package biz

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexCatalogSpeedAliasesPreserveNativeContract(t *testing.T) {
	const source = `{"future_root":true,"models":[{"slug":"gpt-6.1-sol","display_name":"Sol","base_instructions":"native instructions","model_messages":{"future":"retained"},"experimental_supported_tools":["clock"],"service_tiers":[{"id":"priority"}],"default_service_tier":"ultrafast"}]}`
	catalog, err := ParseCodexCatalog([]byte(source))
	require.NoError(t, err)
	raw, err := catalog.Intersect([]string{"gpt-6.1-sol-fast", "gpt-6.1-sol-ultrafast", "gpt-6.1-sol-fast", "unknown-fast"})
	require.NoError(t, err)
	require.Len(t, gjson.GetBytes(raw, "models").Array(), 2)
	for _, model := range gjson.GetBytes(raw, "models").Array() {
		require.Equal(t, "native instructions", model.Get("base_instructions").String())
		require.Equal(t, "retained", model.Get("model_messages.future").String())
		require.Equal(t, "clock", model.Get("experimental_supported_tools.0").String())
		require.Empty(t, model.Get("service_tiers").Array())
		require.Equal(t, gjson.Null, model.Get("default_service_tier").Type)
	}
	require.True(t, gjson.GetBytes(raw, "future_root").Bool())
	unchanged, err := catalog.Intersect([]string{"gpt-6.1-sol"})
	require.NoError(t, err)
	require.JSONEq(t, source, string(unchanged), "one caller's alias must not mutate the shared upstream catalog")
	empty, err := catalog.Intersect([]string{"gpt-6-astra-fast"})
	require.NoError(t, err)
	require.Empty(t, gjson.GetBytes(empty, "models").Array())
}

func TestCodexCatalogSpeedAliasKeepsExplicitUpstreamEntry(t *testing.T) {
	const raw = `{"models":[{"slug":"gpt-6.1-sol-fast","custom":"upstream"}]}`
	catalog, err := ParseCodexCatalog([]byte(raw))
	require.NoError(t, err)
	got, err := catalog.Intersect([]string{"gpt-6.1-sol-fast"})
	require.NoError(t, err)
	require.JSONEq(t, raw, string(got))
}
