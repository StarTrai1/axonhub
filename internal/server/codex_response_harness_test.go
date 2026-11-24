package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/datastorage"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/internal/server/api"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/orchestrator"
	"github.com/looplj/axonhub/llm/httpclient"
)

func setupCodexResponseHandlers(t *testing.T, h *codexRouteHarness, models *biz.ModelService) (*api.OpenAIHandlers, *biz.RequestService) {
	t.Helper()
	cache := xcache.Config{Mode: xcache.ModeMemory}
	h.db.DataStorage.Create().SetName("test").SetDescription("test").SetPrimary(true).SetType(datastorage.TypeDatabase).SetSettings(&objects.DataStorageSettings{}).SaveX(h.ctx)
	storage := &biz.DataStorageService{AbstractService: &biz.AbstractService{}, SystemService: h.system, Cache: xcache.NewFromConfig[ent.DataStorage](cache)}
	usage := biz.NewUsageLogService(h.db, h.system, h.channels)
	requests := biz.NewRequestService(h.db, cache, h.system, usage, storage, biz.NewLiveStreamRegistry())
	prompts := biz.NewPromptService(biz.PromptServiceParams{Ent: h.db})
	protection := biz.NewPromptProtectionRuleService(biz.PromptProtectionRuleServiceParams{Ent: h.db, CacheConfig: cache})
	t.Cleanup(protection.Stop)
	h.usage = usage
	h.prompts = prompts
	h.protect = protection
	h.requests = requests
	require.NoError(t, h.system.SetModelSettings(h.ctx, biz.SystemModelSettings{QueryAllChannelModels: true, FallbackToChannelsOnModelNotFound: true}))
	handlers := api.NewOpenAIHandlers(api.OpenAIHandlersParams{
		ChannelService: h.channels, ModelService: models, SystemService: h.system, Client: h.db,
		DefaultSelector: orchestrator.NewDefaultSelector(h.channels, models, h.system),
		RequestService:  requests, UsageLogService: usage, PromptService: prompts, PromptProtectionRuleService: protection,
		HttpClient: httpclient.NewHttpClient(), LiveStreamRegistry: biz.NewLiveStreamRegistry(),
		ChannelLimiterManager: orchestrator.NewChannelLimiterManager(),
	})
	return handlers, requests
}
