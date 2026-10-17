package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
)

func TestSearchOnlyCandidatesKeepProfileBoundary(t *testing.T) {
	ctx, client := setupTest(t)
	ch, err := client.Channel.Create().SetName("search-only").SetType(channel.TypeCodex).
		SetBaseURL("https://search.example.test/v1").SetCredentials(objects.ChannelCredentials{APIKey: "synthetic"}).
		SetSupportedModels([]string{"gpt-6-sol"}).SetDefaultTestModel("gpt-6-sol").SetStatus(channel.StatusEnabled).
		SetSettings(&objects.ChannelSettings{CodexSearchOnly: true}).Save(ctx)
	require.NoError(t, err)
	_, err = client.Channel.Create().SetName("disabled-search-only").SetType(channel.TypeCodex).
		SetBaseURL("https://disabled.example.test/v1").SetCredentials(objects.ChannelCredentials{APIKey: "synthetic"}).
		SetSupportedModels([]string{"gpt-6-sol"}).SetDefaultTestModel("gpt-6-sol").SetStatus(channel.StatusDisabled).
		SetSettings(&objects.ChannelSettings{CodexSearchOnly: true}).Save(ctx)
	require.NoError(t, err)
	selector := NewDefaultSelector(newTestChannelServiceForChannels(client), newTestModelService(client), newTestSystemService(client))
	req := &llm.Request{Model: "gpt-6-astra", RequestType: llm.RequestTypeSearch, APIFormat: llm.APIFormatOpenAISearch}
	candidates, err := selector.Select(ctx, req)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, ch.ID, candidates[0].Channel.ID)
	require.Equal(t, "gpt-6-astra", candidates[0].Models[0].ActualModel)
	allowed, err := WithSelectedChannelsSelector(selector, []int{ch.ID}).Select(ctx, req)
	require.NoError(t, err)
	require.Len(t, allowed, 1)
	denied, err := WithSelectedChannelsSelector(selector, []int{ch.ID + 100}).Select(ctx, req)
	require.NoError(t, err)
	require.Empty(t, denied)
	for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseWebSocket, llm.APIFormatOpenAIChatCompletion, llm.APIFormatOpenAIResponseCompact} {
		candidates, err := selector.Select(ctx, &llm.Request{Model: "gpt-6-sol", RequestType: llm.RequestTypeChat, APIFormat: format})
		require.NoError(t, err)
		require.Empty(t, candidates, "search-only leaked into %s", format)
	}
}
