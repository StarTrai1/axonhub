package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
)

func TestCodexToolOnlyRoutingMatrix(t *testing.T) {
	for _, settings := range []objects.ChannelSettings{
		{}, {CodexSearchOnly: true}, {CodexImageOnly: true}, {CodexSearchOnly: true, CodexImageOnly: true},
	} {
		ctx, client := setupTest(t)
		ch, err := client.Channel.Create().SetName("tools").SetType(channel.TypeCodex).
			SetBaseURL("https://tools.example.test/v1").SetCredentials(objects.ChannelCredentials{APIKey: "synthetic"}).
			SetSupportedModels([]string{"gpt-6.1-sol", "gpt-image-2.6-sunburst"}).SetDefaultTestModel("gpt-6.1-sol").
			SetStatus(channel.StatusEnabled).SetSettings(&settings).Save(ctx)
		require.NoError(t, err)
		selector := NewDefaultSelector(newTestChannelServiceForChannels(client), newTestModelService(client), newTestSystemService(client))
		restricted := settings.CodexSearchOnly || settings.CodexImageOnly
		for _, tc := range []struct {
			format llm.APIFormat
			kind llm.RequestType
			model string
			allowed bool
		}{
			{llm.APIFormatOpenAIResponse, llm.RequestTypeChat, "gpt-6.1-sol", !restricted},
			{llm.APIFormatOpenAIResponseWebSocket, llm.RequestTypeChat, "gpt-6.1-sol", !restricted},
			{llm.APIFormatOpenAIChatCompletion, llm.RequestTypeChat, "gpt-6.1-sol", !restricted},
			{llm.APIFormatOpenAISearch, llm.RequestTypeSearch, "gpt-6.1-sol", !restricted || settings.CodexSearchOnly},
			{llm.APIFormatOpenAIImageGeneration, llm.RequestTypeImage, "gpt-image-2.6-sunburst", !restricted || settings.CodexImageOnly},
			{llm.APIFormatOpenAIImageEdit, llm.RequestTypeImage, "gpt-image-2.6-sunburst", !restricted || settings.CodexImageOnly},
		} {
			req := &llm.Request{Model: tc.model, RequestType: tc.kind, APIFormat: tc.format}
			candidates, err := selector.Select(ctx, req)
			require.NoError(t, err)
			if tc.allowed {
				require.Len(t, candidates, 1, "%+v / %s", settings, tc.format)
				require.True(t, candidates[0].Channel.AllowsCodexToolRequest(req))
			} else {
				require.Empty(t, candidates, "%+v / %s", settings, tc.format)
			}
			denied, err := WithSelectedChannelsSelector(selector, []int{ch.ID + 100}).Select(ctx, req)
			require.NoError(t, err)
			require.Empty(t, denied)
		}
	}
}
