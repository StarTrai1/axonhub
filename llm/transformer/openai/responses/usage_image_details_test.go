package responses

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesUsageKeepsImageSubsetsWithoutAddingToTotals(t *testing.T) {
	var usage Usage
	raw := []byte(`{"input_tokens":100,"input_tokens_details":{"cached_tokens":20,"image_tokens":30,"text_tokens":70},"output_tokens":40,"output_tokens_details":{"image_tokens":0,"text_tokens":40},"total_tokens":140}`)
	require.NoError(t, json.Unmarshal(raw, &usage))
	normalized := usage.ToUsage()
	require.EqualValues(t, 100, normalized.PromptTokens)
	require.EqualValues(t, 140, normalized.TotalTokens)
	require.EqualValues(t, 30, normalized.PromptTokensDetails.ImageTokens)
	require.NotNil(t, normalized.CompletionTokensDetails.ImageTokens)
	require.EqualValues(t, 0, *normalized.CompletionTokensDetails.ImageTokens)
	roundTrip := ConvertLLMUsageToResponsesUsage(normalized)
	require.Equal(t, usage.InputTokenDetails, roundTrip.InputTokenDetails)
	require.Equal(t, usage.OutputTokenDetails, roundTrip.OutputTokenDetails)

	require.NoError(t, json.Unmarshal([]byte(`{"input_tokens":1,"output_tokens":2,"total_tokens":3}`), &usage))
	require.Nil(t, usage.ToUsage().CompletionTokensDetails.ImageTokens)
}
