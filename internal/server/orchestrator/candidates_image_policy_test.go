package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
)

func TestImagePolicyDelegatesWithExplicitModelAndProfileBoundary(t *testing.T) {
	ctx, client := setupTest(t)
	for _, name := range []string{"conversation", "image-tool"} {
		settings := &objects.ChannelSettings{}
		policies := objects.ChannelPolicies{ImageGeneration: objects.ImageGenerationOtherChannels}
		if name == "image-tool" {
			settings.CodexImageOnly = true
			settings.CodexImageModel = "gpt-image-2.6-sunburst"
			policies.ImageGeneration = objects.ImageGenerationNative
		}
		_, err := client.Channel.Create().SetName(name).SetType(channel.TypeCodex).
			SetBaseURL("https://images.example.test/v1").SetCredentials(objects.ChannelCredentials{APIKey: "synthetic"}).
			SetDefaultTestModel("gpt-image-2").SetSupportedModels([]string{"gpt-image-2", "gpt-image-2.6-sunburst"}).
			SetStatus(channel.StatusEnabled).SetSettings(settings).SetPolicies(policies).Save(ctx)
		require.NoError(t, err)
	}
	selector := NewDefaultSelector(newTestChannelServiceForChannels(client), newTestModelService(client), newTestSystemService(client))
	for _, format := range []llm.APIFormat{llm.APIFormatOpenAIImageGeneration, llm.APIFormatOpenAIImageEdit} {
		req := &llm.Request{Model: "gpt-image-2", RequestType: llm.RequestTypeImage, APIFormat: format}
		got, err := selector.Select(ctx, req)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, "image-tool", got[0].Channel.Name)
		require.Equal(t, "gpt-image-2.6-sunburst", got[0].Models[0].ActualModel)
		require.Equal(t, "gpt-image-2", req.Model)
		denied, err := WithSelectedChannelsSelector(selector, []int{got[0].Channel.ID + 100}).Select(ctx, req)
		require.NoError(t, err)
		require.Empty(t, denied)
	}
}
