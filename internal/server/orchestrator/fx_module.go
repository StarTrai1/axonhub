package orchestrator

import (
	"go.uber.org/fx"

	"github.com/looplj/axonhub/internal/server/biz"
	rt "github.com/looplj/axonhub/internal/server/realtime"
)

var Module = fx.Module("orchestrator",
	fx.Provide(NewDefaultSelector),
	fx.Provide(NewRealtimeService),
	fx.Provide(func() rt.Dialer { return rt.SecureDialer{} }),
	fx.Provide(NewCandidateSelectorDiagnostics),
	fx.Provide(NewChannelLimiterManager),
	fx.Provide(NewTestChannelOrchestrator),
	fx.Provide(func(svc *biz.ProviderQuotaService) ProviderQuotaStatusProvider { return svc }),
)
