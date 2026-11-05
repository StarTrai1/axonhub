package biz

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/providerquotastatus"
	"github.com/looplj/axonhub/internal/server/biz/provider_quota"
)

// CheckQuotaReserve reads current usage for admission, independently of the
// dashboard's periodic cache and collection mutex. Only overlapping checks
// share a result: a later request must observe consumption since the last one.
// It never lists/redeems reset credits or writes periodic collection state.
func (svc *ProviderQuotaService) CheckQuotaReserve(ctx context.Context, channelID int) (*QuotaChannelStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := svc.reserveChecks.DoChan(strconv.Itoa(channelID), func() (any, error) {
		// One caller disconnecting must not cancel other callers' admission.
		checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		// This internal check only receives channels already admitted by the
		// routing plan. Inference keys need not have channel-admin read scopes.
		return authz.RunWithSystemBypass(ent.NewContext(checkCtx, svc.db), "quota-reserve-admission", func(bypassCtx context.Context) (*QuotaChannelStatus, error) {
			return svc.checkQuotaReserve(bypassCtx, channelID)
		})
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case checked := <-result:
		if checked.Err != nil {
			return nil, checked.Err
		}
		status := checked.Val.(*QuotaChannelStatus)
		copy := *status
		copy.Limits = cloneLimits(status.Limits)
		return &copy, nil
	}
}

func (svc *ProviderQuotaService) checkQuotaReserve(ctx context.Context, channelID int) (*QuotaChannelStatus, error) {
	ch, err := svc.db.Channel.Get(ctx, channelID)
	if err != nil || ch.Status != channel.StatusEnabled || (ch.Type != channel.TypeCodex && ch.Type != channel.TypeClaudecode) {
		return nil, errors.New("quota reserve channel is unavailable")
	}
	providerType := svc.getProviderType(ch)
	enabled, err := svc.SystemService.IsProviderQuotaCollectionEnabled(ctx, providerType)
	if err != nil || !enabled {
		return nil, errors.New("quota reserve requires enabled provider quota collection")
	}
	checker, ok := svc.checkers[providerType]
	if !ok || !hasCredentialsForProvider(ch) {
		return nil, errors.New("quota reserve usage credentials are unavailable")
	}
	var data provider_quota.QuotaData
	if reader, ok := checker.(*provider_quota.ClaudeCodeQuotaChecker); ok {
		// Its periodic checker sends a 1-token message to collect headers;
		// admission must use the read-only account usage endpoint instead.
		data, err = reader.CheckUsage(ctx, ch)
	} else {
		data, err = checker.CheckQuota(ctx, ch)
	}
	if err != nil {
		// Provider errors may contain response bodies or credentials. Admission
		// reports a stable error without propagating those details to callers.
		return nil, errors.New("quota reserve provider usage check failed")
	}
	data = provider_quota.NormalizeQuotaData(data)
	return &QuotaChannelStatus{
		ProviderType: providerType,
		Status:       providerquotastatus.Status(data.Status),
		Ready:        data.Ready,
		Limits:       cloneLimits(data.Limits),
	}, nil
}
