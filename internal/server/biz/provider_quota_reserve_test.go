package biz

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/providerquotastatus"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz/provider_quota"
	"github.com/looplj/axonhub/llm/httpclient"
)

type reserveUsageChecker struct {
	check  func(context.Context, *ent.Channel) (provider_quota.QuotaData, error)
	resets atomic.Int32
}

func (c *reserveUsageChecker) CheckQuota(ctx context.Context, ch *ent.Channel) (provider_quota.QuotaData, error) {
	return c.check(ctx, ch)
}

func (c *reserveUsageChecker) SupportsChannel(*ent.Channel) bool { return true }

func (c *reserveUsageChecker) ListResets(context.Context, *ent.Channel) (provider_quota.ResetList, error) {
	c.resets.Add(1)
	return provider_quota.ResetList{}, errors.New("reset credits must not be queried during admission")
}

func (c *reserveUsageChecker) Reset(context.Context, *ent.Channel, string) error {
	return errors.New("reset must not be called during admission")
}

func TestQuotaReserveUsageIgnoresPollingScheduleAndMutex(t *testing.T) {
	svc, _, ctx, client := setupProviderQuotaCollectionService(t)
	defer client.Close()
	ch := createProviderQuotaCollectionChannel(t, ctx, client, "reserve", channel.TypeCodex)
	_, err := client.Channel.UpdateOne(ch).SetCredentials(objects.ChannelCredentials{OAuth: &objects.OAuthCredentials{AccessToken: "synthetic"}}).Save(ctx)
	require.NoError(t, err)
	var calls int
	checker := &reserveUsageChecker{check: func(context.Context, *ent.Channel) (provider_quota.QuotaData, error) {
		calls++
		limits := snapshotTestLimits()
		limits[0].UsageRatio = .94 + float64(calls)/100
		return provider_quota.QuotaData{Status: "warning", Limits: limits}, nil
	}}
	svc.checkers["codex"] = checker
	svc.updateQuotaCache(ch.ID, "codex", providerquotastatus.StatusWarning, true, "warning", snapshotTestLimits())
	svc.mu.Lock()
	t.Cleanup(svc.mu.Unlock)
	for i := range 2 {
		status, err := svc.CheckQuotaReserve(t.Context(), ch.ID)
		require.NoError(t, err)
		require.InDelta(t, .95+float64(i)/100, status.Limits[0].UsageRatio, .0001)
		status.Limits[0].UsageRatio = 0
	}
	require.Equal(t, 2, calls, "a later request cannot reuse an earlier admission sample")
	require.Zero(t, checker.resets.Load())
	require.Equal(t, .5, svc.GetQuotaStatus(ctx, ch.ID).Limits[0].UsageRatio, "admission must not race periodic cache writes")
	count, err := client.ProviderQuotaStatus.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count, "admission must not write periodic quota rows")
}

func TestQuotaReserveUsageSingleflightCancellation(t *testing.T) {
	svc, _, ctx, client := setupProviderQuotaCollectionService(t)
	defer client.Close()
	ch := createProviderQuotaCollectionChannel(t, ctx, client, "reserve-flight", channel.TypeCodex)
	_, err := client.Channel.UpdateOne(ch).SetCredentials(objects.ChannelCredentials{OAuth: &objects.OAuthCredentials{AccessToken: "synthetic"}}).Save(ctx)
	require.NoError(t, err)
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	svc.checkers["codex"] = &reserveUsageChecker{check: func(checkCtx context.Context, _ *ent.Channel) (provider_quota.QuotaData, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return provider_quota.QuotaData{Status: "warning", Limits: snapshotTestLimits()}, checkCtx.Err()
		case <-checkCtx.Done():
			return provider_quota.QuotaData{}, checkCtx.Err()
		}
	}}
	callerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		defer func() {
			if recover() != nil {
				t.Error("quota admission caller panicked")
				done <- errors.New("admission caller panicked")
			}
		}()
		_, err := svc.CheckQuotaReserve(callerCtx, ch.ID)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("usage check did not start")
	}
	// Join the same in-flight read deterministically before releasing it.
	joined := svc.reserveChecks.DoChan(strconv.Itoa(ch.ID), func() (any, error) {
		return nil, errors.New("concurrent admission was not coalesced")
	})
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	close(release)
	result := <-joined
	require.NoError(t, result.Err, "canceling one caller must not cancel shared usage work")
	require.True(t, result.Shared)
	require.EqualValues(t, 1, calls.Load())
}

func TestQuotaReserveUsageFailsClosedWithoutUsableCollection(t *testing.T) {
	for _, reason := range []string{"disabled collection", "disabled channel", "missing credentials", "provider error"} {
		t.Run(reason, func(t *testing.T) {
			svc, system, ctx, client := setupProviderQuotaCollectionService(t)
			defer client.Close()
			ch := createProviderQuotaCollectionChannel(t, ctx, client, "reserve-error", channel.TypeCodex)
			credentials := objects.ChannelCredentials{OAuth: &objects.OAuthCredentials{AccessToken: "synthetic"}}
			if reason == "missing credentials" {
				credentials = objects.ChannelCredentials{}
			}
			_, err := client.Channel.UpdateOne(ch).SetCredentials(credentials).Save(ctx)
			require.NoError(t, err)
			if reason == "disabled channel" {
				_, err = client.Channel.UpdateOne(ch).SetStatus(channel.StatusDisabled).Save(ctx)
				require.NoError(t, err)
			}
			if reason == "disabled collection" {
				require.NoError(t, system.UpdateProviderQuotaCollectionSettings(ctx, lo.ToPtr(false), nil))
			}
			var calls int
			svc.checkers["codex"] = &reserveUsageChecker{check: func(context.Context, *ent.Channel) (provider_quota.QuotaData, error) {
				calls++
				return provider_quota.QuotaData{}, errors.New("sensitive-provider-response")
			}}
			status, err := svc.CheckQuotaReserve(ctx, ch.ID)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "sensitive-provider-response")
			require.Nil(t, status)
			if reason == "provider error" {
				require.Equal(t, 1, calls)
			} else {
				require.Zero(t, calls)
			}
		})
	}
}

func TestQuotaReserveReadRetryIsBoundedAndPreservesDenials(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		after  string
		calls  int
	}{
		{name: "temporary read failure", status: 503, calls: 2},
		{name: "invalid credentials", status: 401, calls: 1},
		{name: "forbidden", status: 403, calls: 1},
		{name: "unsupported endpoint", status: 501, calls: 1},
		{name: "rate limit outlives budget", status: 429, after: "60", calls: 1},
		{name: "retry after outlives budget", status: 503, after: "60", calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, ctx, client := setupProviderQuotaCollectionService(t)
			defer client.Close()
			ch := createProviderQuotaCollectionChannel(t, ctx, client, "reserve-read", channel.TypeCodex)
			_, err := client.Channel.UpdateOne(ch).SetCredentials(objects.ChannelCredentials{OAuth: &objects.OAuthCredentials{AccessToken: "synthetic"}}).Save(ctx)
			require.NoError(t, err)
			var calls int
			svc.checkers["codex"] = &reserveUsageChecker{check: func(context.Context, *ent.Channel) (provider_quota.QuotaData, error) {
				calls++
				return provider_quota.QuotaData{}, &httpclient.Error{StatusCode: tc.status, URL: "https://private.example/secret", Body: []byte("private-body"), Headers: http.Header{"Retry-After": {tc.after}}}
			}}
			status, err := svc.CheckQuotaReserve(ctx, ch.ID)
			require.Nil(t, status)
			require.EqualError(t, err, fmt.Sprintf("usage endpoint returned HTTP %d", tc.status))
			require.Equal(t, tc.calls, calls)
		})
	}
}
