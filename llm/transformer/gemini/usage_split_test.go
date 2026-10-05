package gemini

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestSplitUsageSnapshotsPreserveMissingFields(t *testing.T) {
	chunks := []*httpclient.StreamEvent{
		{Data: []byte(`{"responseId":"split","usageMetadata":{"promptTokenCount":100,"cachedContentTokenCount":40,"cost":"0.25"},"candidates":[{"content":{"role":"model","parts":[{"text":"answer"}]}}]}`)},
		{Data: []byte(`{"usageMetadata":{"candidatesTokenCount":10,"thoughtsTokenCount":5},"candidates":[{"finishReason":"MAX_TOKENS"}]}`)},
		{Data: []byte(`{"usageMetadata":{"cachedContentTokenCount":0,"totalTokenCount":115}}`)},
	}
	adapter := &OutboundTransformer{}
	stream, err := adapter.TransformStream(t.Context(), &httpclient.Request{}, streams.SliceStream(chunks))
	require.NoError(t, err)
	defer stream.Close()
	var lastUsage *llm.Usage
	for stream.Next() {
		if response := stream.Current(); response != nil && response.Usage != nil {
			lastUsage = response.Usage
		}
	}
	require.NoError(t, stream.Err())
	require.NotNil(t, lastUsage)
	require.EqualValues(t, 100, lastUsage.PromptTokens)
	require.EqualValues(t, 15, lastUsage.CompletionTokens)
	require.EqualValues(t, 115, lastUsage.TotalTokens)
	body, meta, err := AggregateStreamChunks(t.Context(), chunks)
	require.NoError(t, err)
	var response GenerateContentResponse
	require.NoError(t, json.Unmarshal(body, &response))
	require.EqualValues(t, 100, response.UsageMetadata.PromptTokenCount)
	require.EqualValues(t, 0, response.UsageMetadata.CachedContentTokenCount, "an explicit zero replaces the previous count")
	require.EqualValues(t, 5, response.UsageMetadata.ThoughtsTokenCount)
	require.Equal(t, 0.25, *response.UsageMetadata.Cost)
	require.Equal(t, "MAX_TOKENS", response.Candidates[0].FinishReason)
	require.EqualValues(t, 100, meta.Usage.PromptTokens)
}
