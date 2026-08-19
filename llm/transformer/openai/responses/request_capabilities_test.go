package responses

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequestPreservesGPT56ReasoningMode(t *testing.T) {
	request := &Request{
		Model: "gpt-5.6-sol",
		Reasoning: &Reasoning{
			Effort: "high",
			Mode:   "pro",
		},
	}

	unified, err := convertToLLMRequest(request)
	require.NoError(t, err)
	require.Equal(t, "pro", unified.ReasoningMode)
	reasoning := convertReasoning(unified)
	require.NotNil(t, reasoning)
	require.Equal(t, "high", reasoning.Effort)
	require.Equal(t, "pro", reasoning.Mode)
}
