package gemini

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestGeminiQuotedUsageSurvivesResponseAndStream(t *testing.T) {
	body := []byte(`{"responseId":"resp_quoted","modelVersion":"gemini-3.8-flash","candidates":[{"content":{"role":"model","parts":[{"text":"answer"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":"8383","toolUsePromptTokenCount":"2","candidatesTokenCount":"164","thoughtsTokenCount":"2","totalTokenCount":"8551","cachedContentTokenCount":"20","promptTokensDetails":[{"modality":"TEXT","tokenCount":"8383"}],"cost":0.125}}`)
	transformer := &OutboundTransformer{}
	response, err := transformer.TransformResponse(t.Context(), &httpclient.Response{StatusCode: 200, Body: body})
	require.NoError(t, err)
	require.EqualValues(t, 8385, response.Usage.PromptTokens)
	require.EqualValues(t, 166, response.Usage.CompletionTokens)
	require.EqualValues(t, 20, response.Usage.PromptTokensDetails.CachedTokens)
	require.EqualValues(t, 2, response.Usage.CompletionTokensDetails.ReasoningTokens)
	chunk, err := transformer.TransformStreamChunk(t.Context(), &httpclient.StreamEvent{Data: body})
	require.NoError(t, err)
	require.Equal(t, response.Usage, chunk.Usage)
	require.Equal(t, response.Model, chunk.Model)
	var decoded GenerateContentResponse
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.Equal(t, 0.125, *decoded.UsageMetadata.Cost)
	require.EqualValues(t, 8383, decoded.UsageMetadata.PromptTokensDetails[0].TokenCount)
	encoded, err := json.Marshal(decoded.UsageMetadata)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"promptTokenCount":8383`)
}

func TestGeminiQuotedUsageRejectsInvalidCounts(t *testing.T) {
	for _, value := range []string{`""`, `"1.5"`, `"-1"`, `"9223372036854775808"`, `true`, `{}`} {
		var usage UsageMetadata
		require.Error(t, json.Unmarshal([]byte(`{"promptTokenCount":`+value+`}`), &usage), value)
	}
	for _, value := range []string{`17`, `"17"`, `null`} {
		var usage UsageMetadata
		require.NoError(t, json.Unmarshal([]byte(`{"promptTokenCount":`+value+`}`), &usage), value)
	}
	var usage UsageMetadata
	require.NoError(t, json.Unmarshal([]byte(`{"promptTokenCount":"9007199254740993"}`), &usage))
	require.EqualValues(t, 9007199254740993, usage.PromptTokenCount)
}
