package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/server/biz"
)

func TestUnifiedHistoryInputAllowanceKeepsOutputAndConfiguredWindows(t *testing.T) {
	_, _, state, _ := unifiedHistoryFixture(t)
	candidate := state.CurrentCandidate
	contextTokens := biz.BundledModelContextLimit("gpt-6-astra")
	target, ceiling := unifiedHistoryInputBudgets([]byte(`{"model":"gpt-6-astra"}`), candidate, 0, contextTokens)
	require.Equal(t, contextTokens-contextTokens/4, target)
	require.Greater(t, ceiling, target)
	require.Less(t, ceiling, biz.BundledModelHistoryInputLimit("gpt-6-astra", contextTokens), "leave room for framing too")
	smallTarget, smallCeiling := unifiedHistoryInputBudgets([]byte(`{}`), candidate, 0, 372000)
	require.Equal(t, 279000, smallTarget)
	require.Equal(t, smallTarget, smallCeiling, "a different configured window cannot inherit the catalog contract")
	candidate.Models[0].ActualModel = "tenant/gpt-6-astra"
	unknownTarget, unknownCeiling := unifiedHistoryInputBudgets([]byte(`{}`), candidate, 0, contextTokens)
	require.Equal(t, target, unknownTarget)
	require.Equal(t, unknownTarget, unknownCeiling)
	candidate.Models[0].ActualModel = "gpt-6-astra"
	overriddenTarget, overriddenCeiling := unifiedHistoryInputBudgets([]byte(`{"model":"another-destination"}`), candidate, 0, contextTokens)
	require.Equal(t, target, overriddenTarget)
	require.Equal(t, overriddenTarget, overriddenCeiling, "a body override cannot borrow another model's larger input limit")
	_, reserved := unifiedHistoryInputBudgets([]byte(`{"max_output_tokens":300000}`), candidate, 0, contextTokens)
	require.LessOrEqual(t, reserved, contextTokens-300000-4096)
	noInput, noCeiling := unifiedHistoryInputBudgets([]byte(`{"max_output_tokens":2000000}`), candidate, 0, contextTokens)
	require.Zero(t, noInput)
	require.Zero(t, noCeiling)
}
