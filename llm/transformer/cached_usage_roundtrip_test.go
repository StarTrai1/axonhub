package transformer_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestCachedTokenDetailsSurviveProtocolAndPersistenceRoundTrip(t *testing.T) {
	for _, details := range []string{
		`{"cached_tokens":80,"cache_write_tokens":10}`,
		`{"cached_tokens":80,"cache_write_tokens":10,"cached_tokens_details":{}}`,
		`{"cached_tokens":80,"cache_write_tokens":10,"cached_tokens_details":{"text_tokens":0,"image_tokens":80}}`,
		`{"cached_tokens":80,"cache_write_tokens":10,"cached_tokens_details":{"text_tokens":30,"image_tokens":40,"audio_tokens":10}}`,
	} {
		t.Run(details, func(t *testing.T) {
			var original responses.Usage
			require.NoError(t, json.Unmarshal([]byte(`{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":`+details+`}`), &original))
			stored, err := json.Marshal(original.ToUsage())
			require.NoError(t, err)
			var restored llm.Usage
			require.NoError(t, json.Unmarshal(stored, &restored))
			chat := openai.UsageFromLLM(&restored)
			chatBody, err := json.Marshal(chat)
			require.NoError(t, err)
			var decodedChat openai.Usage
			require.NoError(t, json.Unmarshal(chatBody, &decodedChat))
			result := responses.ConvertLLMUsageToResponsesUsage(decodedChat.ToLLMUsage())
			resultDetails, err := json.Marshal(result.InputTokenDetails)
			require.NoError(t, err)
			require.JSONEq(t, details, string(resultDetails))
			require.Equal(t, int64(100), result.InputTokens)
			require.Equal(t, int64(20), result.OutputTokens)
			require.Equal(t, int64(120), result.TotalTokens, "cached modality counts are subsets and must not be added to totals")
		})
	}
}

func TestCachedTokenDetailsDoNotShareMutableSnapshots(t *testing.T) {
	var original responses.Usage
	require.NoError(t, json.Unmarshal([]byte(`{"input_tokens_details":{"cached_tokens":80,"cached_tokens_details":{"image_tokens":80,"text_tokens":0}}}`), &original))
	unified := original.ToUsage()
	*unified.PromptTokensDetails.CachedTokensDetails.ImageTokens = 70
	require.Equal(t, int64(80), *original.InputTokenDetails.CachedTokensDetails.ImageTokens)
	chat := openai.UsageFromLLM(unified)
	*chat.PromptTokensDetails.CachedTokensDetails.ImageTokens = 60
	require.Equal(t, int64(70), *unified.PromptTokensDetails.CachedTokensDetails.ImageTokens)
	converted := chat.ToLLMUsage()
	*converted.PromptTokensDetails.CachedTokensDetails.ImageTokens = 50
	require.Equal(t, int64(60), *chat.PromptTokensDetails.CachedTokensDetails.ImageTokens)
	response := responses.ConvertLLMUsageToResponsesUsage(converted)
	*response.InputTokenDetails.CachedTokensDetails.TextTokens = 10
	require.Zero(t, *converted.PromptTokensDetails.CachedTokensDetails.TextTokens)
	require.Nil(t, response.InputTokenDetails.CachedTokensDetails.AudioTokens)
}

func TestCachedTokenDetailsUnmarshalClearsPreviousSnapshot(t *testing.T) {
	var details openai.PromptTokensDetails
	require.NoError(t, json.Unmarshal([]byte(`{"cached_tokens_details":{"image_tokens":80}}`), &details))
	require.NotNil(t, details.CachedTokensDetails)
	require.NoError(t, json.Unmarshal([]byte(`{"cached_tokens":0}`), &details))
	require.Nil(t, details.CachedTokensDetails)
}
