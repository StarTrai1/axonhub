package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
)

func TestFunctionToolPreservesDeferredLoadingOptions(t *testing.T) {
	deferred := true
	strict := true
	requestTool := Tool{
		Name:           "lookup",
		Description:    "Look up a record",
		InputSchema:    json.RawMessage(`{"type":"object","properties":{}}`),
		Strict:         &strict,
		AllowedCallers: []string{"direct", "code_execution_20260120"},
		DeferLoading:   &deferred,
		InputExamples: []json.RawMessage{
			json.RawMessage(`{"query":"example"}`),
		},
	}

	unified, ok := convertToolToLLM(requestTool)
	require.True(t, ok)
	require.Equal(t, requestTool.AllowedCallers, unified.AllowedCallers)
	require.Equal(t, requestTool.DeferLoading, unified.DeferLoading)
	require.Equal(t, requestTool.InputExamples, unified.InputExamples)
	require.Equal(t, requestTool.Strict, unified.Function.Strict)

	roundTripped := convertToolsAnthropic([]llm.Tool{unified}, nil)
	require.Len(t, roundTripped, 1)
	require.Equal(t, requestTool.AllowedCallers, roundTripped[0].AllowedCallers)
	require.Equal(t, requestTool.DeferLoading, roundTripped[0].DeferLoading)
	require.Equal(t, requestTool.InputExamples, roundTripped[0].InputExamples)
	require.Equal(t, requestTool.Strict, roundTripped[0].Strict)
}
