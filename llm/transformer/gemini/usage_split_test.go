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

func TestStreamUsageInvalidChunkDoesNotMutateSnapshot(t *testing.T) {
	previous := &UsageMetadata{PromptTokenCount: 100}
	_, err := unmarshalStreamResponse([]byte(`{"usageMetadata":{"promptTokenCount":200},"candidates":"invalid"}`), previous)
	require.Error(t, err)
	require.EqualValues(t, 100, previous.PromptTokenCount)
}

func TestStreamUsageExplicitNullCostClearsSnapshot(t *testing.T) {
	var previous UsageMetadata
	require.NoError(t, json.Unmarshal([]byte(`{"cost":"0.25"}`), &previous))
	response, err := unmarshalStreamResponse([]byte(`{"usageMetadata":{"cost":null}}`), &previous)
	require.NoError(t, err)
	require.Nil(t, response.UsageMetadata.Cost)
	require.NotNil(t, previous.Cost)
}

func TestStreamModalityUsageOverlaysWithoutMutatingPriorFrames(t *testing.T) {
	first, err := unmarshalStreamResponse([]byte(`{"usageMetadata":{"promptTokensDetails":[{"modality":"TEXT","tokenCount":10},{"modality":"IMAGE","tokenCount":1089}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":2}]}}`), nil)
	require.NoError(t, err)
	snapshot, err := json.Marshal(first.UsageMetadata)
	require.NoError(t, err)
	second, err := unmarshalStreamResponse([]byte(`{"usageMetadata":{"promptTokensDetails":[{"modality":"text","tokenCount":20}],"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":5}]}}`), first.UsageMetadata)
	require.NoError(t, err)
	after, err := json.Marshal(first.UsageMetadata)
	require.NoError(t, err)
	require.JSONEq(t, string(snapshot), string(after))
	require.Equal(t, []*ModalityTokenCount{{Modality: "IMAGE", TokenCount: 1089}, {Modality: "text", TokenCount: 20}}, second.UsageMetadata.PromptTokensDetails)
	require.EqualValues(t, 5, second.UsageMetadata.CandidatesTokensDetails[0].TokenCount)
	third, err := unmarshalStreamResponse([]byte(`{"usageMetadata":{"promptTokensDetails":[{"modality":"IMAGE","tokenCount":0},{"modality":"TEXT","tokenCount":12},{"modality":"TEXT","tokenCount":8}]}}`), second.UsageMetadata)
	require.NoError(t, err)
	require.Len(t, third.UsageMetadata.PromptTokensDetails, 3)
	require.Zero(t, third.UsageMetadata.PromptTokensDetails[0].TokenCount)
	repeated, err := unmarshalStreamResponse([]byte(`{"usageMetadata":{"promptTokensDetails":[{"modality":"IMAGE","tokenCount":0},{"modality":"TEXT","tokenCount":12},{"modality":"TEXT","tokenCount":8}]}}`), third.UsageMetadata)
	require.NoError(t, err)
	require.Equal(t, third.UsageMetadata, repeated.UsageMetadata)
	_, err = unmarshalStreamResponse([]byte(`{"usageMetadata":{"promptTokensDetails":[{"modality":"TEXT","tokenCount":999}]},"candidates":"invalid"}`), first.UsageMetadata)
	require.Error(t, err)
	after, err = json.Marshal(first.UsageMetadata)
	require.NoError(t, err)
	require.JSONEq(t, string(snapshot), string(after))
}
