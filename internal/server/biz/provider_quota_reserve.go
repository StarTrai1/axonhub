package biz

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/providerquotastatus"
	"github.com/looplj/axonhub/internal/server/biz/provider_quota"
	"github.com/looplj/axonhub/llm/httpclient"
)

// QuotaReserveCheckError contains only a local classification, never provider
// response bodies, URLs or credential values. It can safely reach request details.
type QuotaReserveCheckError struct{ message string }

func (e *QuotaReserveCheckError) Error() string { return e.message }

func quotaReserveCheckFailure(err error) error {
	message := "provider usage query failed"
	if upstream, ok := errors.AsType[*httpclient.Error](err); ok {
		message = fmt.Sprintf("usage endpoint returned HTTP %d", upstream.StatusCode)
	} else if errors.Is(err, context.DeadlineExceeded) {
		message = "provider usage query timed out"
	} else if _, ok := errors.AsType[net.Error](err); ok || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		message = "provider usage connection failed"
	}
	return &QuotaReserveCheckError{message: message}
}

func quotaReserveReadRetryDelay(err error) (time.Duration, bool) {
	if err == nil || errors.Is(err, context.Canceled) {
		return 0, false
	}
	if upstream, ok := errors.AsType[*httpclient.Error](err); ok {
		if !httpclient.IsHTTPStatusCodeRetryable(upstream.StatusCode) || upstream.StatusCode == http.StatusNotImplemented {
			return 0, false
		}
		if upstream.StatusCode == http.StatusTooManyRequests || upstream.Headers.Get("Retry-After") != "" {
			// Use the existing bounded Retry-After parser for both 429 and 503.
			advice := &httpclient.Error{StatusCode: http.StatusTooManyRequests, Headers: upstream.Headers}
			delay, _ := httpclient.ParseRetryAfter(advice)
			return delay, true
		}
		return 200 * time.Millisecond, true
	}
	if network, ok := errors.AsType[net.Error](err); ok && (network.Timeout() || network.Temporary()) {
		return 200 * time.Millisecond, true
	}
	if _, ok := errors.AsType[*net.OpError](err); ok || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return 200 * time.Millisecond, true
	}
	return 0, false
}

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
		return authz.RunAsHost(checkCtx, svc.db, "quota-reserve-admission", func(hostCtx context.Context) (*QuotaChannelStatus, error) {
			return svc.checkQuotaReserve(hostCtx, channelID)
		})
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case checked := <-result:
		if checked.Err != nil {
			return nil, checked.Err
		}
		status, ok := checked.Val.(*QuotaChannelStatus)
		if !ok || status == nil {
			return nil, errors.New("quota reserve usage response is unavailable")
		}
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
	readUsage := func() (provider_quota.QuotaData, error) {
		if reader, ok := checker.(*provider_quota.ClaudeCodeQuotaChecker); ok {
			// Its periodic checker sends a 1-token message to collect headers;
			// admission must use the read-only account usage endpoint instead.
			return reader.CheckUsage(ctx, ch)
		}
		return checker.CheckQuota(ctx, ch)
	}
	data, err := readUsage()
	// Recover one transient read failure inside the same five-second budget.
	// This retries only usage GETs, never inference or reset operations, and
	// never authorizes a request from a stale dashboard sample.
	if delay, retry := quotaReserveReadRetryDelay(err); retry && ctx.Err() == nil {
		deadline, bounded := ctx.Deadline()
		if bounded && time.Until(deadline) > delay {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, quotaReserveCheckFailure(ctx.Err())
			case <-timer.C:
				data, err = readUsage()
			}
		}
	}
	if err != nil {
		return nil, quotaReserveCheckFailure(err)
	}
	data = provider_quota.NormalizeQuotaData(data)
	return &QuotaChannelStatus{
		ProviderType: providerType,
		Status:       providerquotastatus.Status(data.Status),
		Ready:        data.Ready,
		Limits:       cloneLimits(data.Limits),
	}, nil
}
