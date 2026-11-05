package orchestrator

import (
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
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

type reserveQuotaTransport func(*http.Request) (*http.Response, error)

func (f reserveQuotaTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
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
}
