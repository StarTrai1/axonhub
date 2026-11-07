package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/providerquotastatus"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/biz/provider_quota"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

type reserveQuotaTransport func(*http.Request) (*http.Response, error)

func (f reserveQuotaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestQuotaReserveTransientUsageFailureRecoversBeforeMemoryDispatch(t *testing.T) {
	ctx, client := setupTest(t)
	token, err := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "reserve-test"},
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	entity := createRetryGuardChannel(t, ctx, client, "reserve-memory", objects.ChannelCredentials{
		OAuth: &objects.OAuthCredentials{AccessToken: token},
	})
	settings := &objects.ChannelSettings{CodexSearchOnly: true, CodexImageOnly: true,
		QuotaReserve: &objects.ChannelQuotaReserve{FiveHourPercent: lo.ToPtr(95), SevenDayPercent: lo.ToPtr(98)},
	}
	entity, err = client.Channel.UpdateOneID(entity.ID).SetType(channel.TypeCodex).
		SetPolicies(objects.ChannelPolicies{CodexMemory: objects.CodexMemoryNormal}).SetSettings(settings).Save(ctx)
	require.NoError(t, err)
	for _, percent := range []int{0, 95} {
		t.Run(fmt.Sprint(percent), func(t *testing.T) {
			var checks atomic.Int32
			hc := httpclient.NewHttpClientWithClient(&http.Client{Transport: reserveQuotaTransport(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, http.MethodGet, req.Method)
				require.Equal(t, "/backend-api/wham/usage", req.URL.Path, "admission must never send an inference")
				if checks.Add(1) == 1 {
					return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"temporary"}`))}, nil
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"rate_limit":{"allowed":true,"primary_window":{"used_percent":%d,"limit_window_seconds":18000,"reset_after_seconds":18000},"secondary_window":{"used_percent":38,"limit_window_seconds":604800,"reset_after_seconds":604800}}}`, percent)))}, nil
			})})
			quota := biz.NewProviderQuotaService(biz.ProviderQuotaServiceParams{Ent: client, SystemService: newTestSystemService(client), HttpClient: hc})
			err := checkQuotaReserveForDispatch(ctx, &biz.Channel{Channel: entity}, &llm.Request{Model: "gpt-6.1-terra"}, true, quota)
			if percent == 0 {
				require.NoError(t, err, "normal memory must recover after a transient read failure when fresh usage is below its limits")
			} else {
				var responseErr *llm.ResponseError
				require.ErrorAs(t, err, &responseErr)
				require.Equal(t, "quota_reserve_reached", responseErr.Detail.Code, "recovery must still enforce the newly observed threshold")
			}
			require.EqualValues(t, 2, checks.Load())
		})
	}
}

func TestQuotaReserveRefreshesBelowThresholdSnapshotBeforeRetry(t *testing.T) {
	ctx, client := setupTest(t)
	token, err := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "reserve-test"},
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	entity := createRetryGuardChannel(t, ctx, client, "reserve-live", objects.ChannelCredentials{
		OAuth: &objects.OAuthCredentials{AccessToken: token},
	})
	_, err = client.Channel.UpdateOneID(entity.ID).
		SetType(channel.TypeCodex).
		SetSettings(&objects.ChannelSettings{QuotaReserve: &objects.ChannelQuotaReserve{FiveHourPercent: lo.ToPtr(95)}}).
		Save(ctx)
	require.NoError(t, err)
	reset := time.Now().Add(time.Hour)
	_, err = client.ProviderQuotaStatus.Create().
		SetChannelID(entity.ID).
		SetProviderType(providerquotastatus.ProviderTypeCodex).
		SetStatus(providerquotastatus.StatusWarning).
		SetReady(true).
		SetUpdatedAt(time.Now().Add(-10 * time.Minute)).
		SetNextCheckAt(time.Now().Add(10 * time.Minute)).
		SetQuotaData(map[string]any{"_limits": []map[string]any{{
			"type": "token", "window": "5h", "usageRatio": .94, "status": "warning", "ready": true, "nextResetAt": reset,
		}}}).Save(ctx)
	require.NoError(t, err)
	var checks atomic.Int32
	hc := httpclient.NewHttpClientWithClient(&http.Client{Transport: reserveQuotaTransport(func(req *http.Request) (*http.Response, error) {
		checks.Add(1)
		if req.Method != http.MethodGet || req.URL.Path != "/backend-api/wham/usage" {
			return nil, fmt.Errorf("unexpected quota request path: %s", req.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"rate_limit":{"allowed":true,"primary_window":{"used_percent":95,"limit_window_seconds":18000,"reset_at":%d}}}`, reset.Unix()))),
		}, nil
	})})
	quota := biz.NewProviderQuotaService(biz.ProviderQuotaServiceParams{Ent: client, SystemService: newTestSystemService(client), HttpClient: hc})
	require.Equal(t, .94, quota.GetQuotaStatus(ctx, entity.ID).Limits[0].UsageRatio)
	svc := newTestChannelServiceForChannels(client)
	p := newRetryGuardTransformer(svc, retryGuardSnapshot(t, ctx, svc, entity.ID), "")
	p.state.quotaReserveProvider = quota
	p.state.LlmRequest = &llm.Request{Model: "test-model"}
	// The real service has a persisted 94% snapshot whose next poll is still
	// ten minutes away. The upstream already reports the configured 95% cap.
	require.ErrorIs(t, p.refreshChannelBeforeRetry(ctx), errChannelUnavailableForRetry,
		"a protected ordinary retry must not use the stale below-threshold snapshot")
	require.EqualValues(t, 1, checks.Load(), "admission must query usage, without listing reset credits or sending inference")
	p.state.ChannelModelsCandidates = []*ChannelModelsCandidate{p.state.CurrentCandidate}
	_, err = p.TransformRequest(ctx, p.state.LlmRequest)
	var quotaErr *llm.ResponseError
	require.ErrorAs(t, err, &quotaErr)
	require.Equal(t, "quota_reserve_reached", quotaErr.Detail.Code)
	require.False(t, p.CanRetry(err), "quota admission must skip same-channel retries")
	require.EqualValues(t, 2, checks.Load(), "final dispatch also verifies fresh usage")
}

type liveQuotaProvider struct {
	mockQuotaStatusProvider

	status *biz.QuotaChannelStatus
	err    error
	calls  int
}

func (p *liveQuotaProvider) CheckQuotaReserve(context.Context, int) (*biz.QuotaChannelStatus, error) {
	p.calls++
	return p.status, p.err
}

func TestQuotaReserveLiveAdmission(t *testing.T) {
	ch := quotaRoutingCandidate(1, objects.QuotaRoutingModeIgnoreQuota).Channel
	ch.Type = channel.TypeCodex
	ch.Settings.QuotaReserve = &objects.ChannelQuotaReserve{FiveHourPercent: lo.ToPtr(95), SevenDayPercent: lo.ToPtr(98)}
	req := &llm.Request{Model: "test-model"}
	for _, tc := range []struct {
		name       string
		five, week float64
		missing    bool
		reset      bool
		err        error
		code       string
	}{
		{name: "below both", five: .94, week: .97},
		{name: "five-hour limit", five: .95, week: .1, code: "quota_reserve_reached"},
		{name: "seven-day limit", five: .1, week: .98, code: "quota_reserve_reached"},
		{name: "missing configured window", five: .1, missing: true, code: "quota_reserve_unavailable"},
		{name: "expired usage needs a fresh window", reset: true, code: "quota_reserve_unavailable"},
		{name: "usage endpoint failed", err: errors.New("synthetic failure"), code: "quota_reserve_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reset := time.Now().Add(time.Hour)
			if tc.reset {
				reset = time.Now().Add(-time.Second)
			}
			status := &biz.QuotaChannelStatus{Limits: []provider_quota.QuotaLimitStatus{
				{Type: provider_quota.QuotaLimitTypeToken, Window: provider_quota.QuotaWindow5h, Status: "available", UsageRatio: tc.five, NextResetAt: &reset},
			}}
			if !tc.missing {
				status.Limits = append(status.Limits, provider_quota.QuotaLimitStatus{Type: provider_quota.QuotaLimitTypeToken, Window: provider_quota.QuotaWindow7d, Status: "available", UsageRatio: tc.week, NextResetAt: &reset})
			}
			provider := &liveQuotaProvider{status: status, err: tc.err}
			err := checkQuotaReserveForDispatch(t.Context(), ch, req, false, provider)
			if tc.code == "" {
				require.NoError(t, err)
			} else {
				var responseErr *llm.ResponseError
				require.ErrorAs(t, err, &responseErr)
				require.Equal(t, tc.code, responseErr.Detail.Code)
			}
			require.Equal(t, 1, provider.calls)
		})
	}
	require.Error(t, checkQuotaReserveForDispatch(t.Context(), ch, req, false, nil))
	ch.Settings.QuotaReserve.AllowSearch = true
	ch.Settings.QuotaReserve.AllowImage = true
	ch.Settings.QuotaReserve.AllowMemory = true
	provider := &liveQuotaProvider{err: errors.New("unavailable")}
	for _, toolReq := range []*llm.Request{
		{RequestType: llm.RequestTypeSearch, APIFormat: llm.APIFormatOpenAISearch},
		{RequestType: llm.RequestTypeImage, APIFormat: llm.APIFormatOpenAIImageGeneration},
	} {
		require.NoError(t, checkQuotaReserveForDispatch(t.Context(), ch, toolReq, false, provider))
	}
	require.NoError(t, checkQuotaReserveForDispatch(t.Context(), ch, req, true, provider))
	require.Zero(t, provider.calls, "retained tools must not run ordinary admission probes")
	require.Error(t, checkQuotaReserveForDispatch(t.Context(), ch, req, false, provider), "ordinary chat remains protected")
	ch.Settings.QuotaReserve = nil
	require.NoError(t, checkQuotaReserveForDispatch(t.Context(), ch, req, false, provider))
	require.Equal(t, 1, provider.calls, "disabled reserves do not add upstream calls")
}
