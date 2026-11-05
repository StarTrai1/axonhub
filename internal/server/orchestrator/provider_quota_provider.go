package orchestrator

import (
	"context"

	"github.com/looplj/axonhub/internal/server/biz"
)

// ProviderQuotaStatusProvider provides quota status information for channels.
type ProviderQuotaStatusProvider interface {
	GetQuotaStatus(ctx context.Context, channelID int) *biz.QuotaChannelStatus
}

// ProviderQuotaReserveChecker verifies explicit reserves at dispatch. A cached
// status provider alone cannot authorize protected requests.
type ProviderQuotaReserveChecker interface {
	CheckQuotaReserve(ctx context.Context, channelID int) (*biz.QuotaChannelStatus, error)
}
