package biz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
)

func TestChannelDedicatedWorkloadsCompose(t *testing.T) {
	for flags := 0; flags < 8; flags++ {
		ch := &Channel{Channel: &ent.Channel{Type: channel.TypeCodex, Settings: &objects.ChannelSettings{CodexSearchOnly: flags&1 != 0, CodexImageOnly: flags&2 != 0}}}
		if flags&4 != 0 {
			ch.Policies.CodexMemory = objects.CodexMemoryOnly
		}
		require.Equal(t, flags == 0, ch.AllowsWorkload(&llm.Request{RequestType: llm.RequestTypeChat}, false), "flags=%d", flags)
		require.Equal(t, flags == 0 || flags&1 != 0, ch.AllowsWorkload(&llm.Request{RequestType: llm.RequestTypeSearch, APIFormat: llm.APIFormatOpenAISearch}, false), "flags=%d", flags)
		require.Equal(t, flags == 0 || flags&2 != 0, ch.AllowsWorkload(&llm.Request{RequestType: llm.RequestTypeImage, APIFormat: llm.APIFormatOpenAIImageGeneration}, false), "flags=%d", flags)
		require.Equal(t, flags == 0 || flags&4 != 0, ch.AllowsWorkload(&llm.Request{RequestType: llm.RequestTypeChat}, true), "flags=%d", flags)
		ch.Policies.CodexMemory = objects.CodexMemoryExclude
		require.False(t, ch.AllowsWorkload(&llm.Request{}, true))
	}
}
