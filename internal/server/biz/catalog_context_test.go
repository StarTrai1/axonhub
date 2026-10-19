package biz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBundledModelContextLimitWithoutImportedModels(t *testing.T) {
	require.Positive(t, BundledModelContextLimit("gpt-6-astra"))
	require.Zero(t, BundledModelContextLimit("unknown-model-without-a-window"))
	require.Zero(t, BundledModelContextLimit("tenant/gpt-6-astra"), "do not guess through an unrelated alias")
}
