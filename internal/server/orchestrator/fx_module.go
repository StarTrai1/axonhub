package orchestrator

import (
	"context"

	"github.com/looplj/axonhub/internal/server/biz"
	"go.uber.org/fx"
)

var Module = fx.Module("orchestrator",
	fx.Provide(NewDefaultSelector),
	fx.Provide(NewCandidateSelectorDiagnostics),
	fx.Provide(NewChannelLimiterManager),
	fx.Provide(NewTestChannelOrchestrator),
	fx.Provide(NewScheduledChannelTestService),
	fx.Provide(func(svc *biz.ProviderQuotaService) ProviderQuotaStatusProvider { return svc }),
	fx.Invoke(func(lc fx.Lifecycle, svc *ScheduledChannelTestService) {
		lc.Append(fx.Hook{
			OnStart: func(ctx context.Context) error {
				return svc.Start(ctx)
			},
		})
	}),
)
