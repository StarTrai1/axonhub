package provider_quota

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/oauth"
)

// CheckUsage reads subscription usage without the inference request made by
// CheckQuota. Keep the channel origin so relay credentials never go elsewhere.
func (c *ClaudeCodeQuotaChecker) CheckUsage(ctx context.Context, ch *ent.Channel) (QuotaData, error) {
	var accessToken string
	if ch.Credentials.OAuth != nil {
		accessToken = ch.Credentials.OAuth.AccessToken
	} else {
		creds, err := oauth.ParseCredentialsJSON(ch.Credentials.APIKey)
		if err != nil {
			return QuotaData{}, fmt.Errorf("invalid usage credentials")
		}
		accessToken = creds.AccessToken
	}
	if accessToken == "" {
		return QuotaData{}, fmt.Errorf("missing usage access token")
	}
	baseURL := ch.BaseURL
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://api.anthropic.com"
	}
	endpoint, err := url.Parse(baseURL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "https" && endpoint.Scheme != "http") || endpoint.User != nil {
		return QuotaData{}, fmt.Errorf("invalid usage endpoint")
	}
	endpoint.Path, endpoint.RawPath, endpoint.RawQuery, endpoint.Fragment = "/api/oauth/usage", "", "", ""
	req := httpclient.NewRequestBuilder().WithMethod("GET").WithURL(endpoint.String()).
		WithBearerToken(accessToken).WithHeader("anthropic-beta", "oauth-2025-04-20").Build()
	hc := c.httpClient
	if ch.Settings != nil && ch.Settings.Proxy != nil {
		hc = hc.WithProxy(ch.Settings.Proxy)
	}
	response, err := hc.Do(ctx, req)
	if err != nil {
		return QuotaData{}, fmt.Errorf("usage request failed")
	}
	return parseClaudeCodeUsage(response.Body)
}

func parseClaudeCodeUsage(body []byte) (QuotaData, error) {
	type usageWindow struct {
		Utilization *float64 `json:"utilization"`
		ResetsAt    string   `json:"resets_at"`
	}
	var windows struct {
		FiveHour *usageWindow `json:"five_hour"`
		SevenDay *usageWindow `json:"seven_day"`
	}
	if err := json.Unmarshal(body, &windows); err != nil {
		return QuotaData{}, fmt.Errorf("invalid usage response")
	}
	data := QuotaData{ProviderType: "claudecode", Status: "unknown"}
	for _, entry := range []struct {
		value  *usageWindow
		window string
	}{{windows.FiveHour, QuotaWindow5h}, {windows.SevenDay, QuotaWindow7d}} {
		window := entry.value
		if window == nil || window.Utilization == nil || math.IsNaN(*window.Utilization) || math.IsInf(*window.Utilization, 0) || *window.Utilization < 0 {
			continue
		}
		var reset *time.Time
		if window.ResetsAt != "" {
			parsed, err := time.Parse(time.RFC3339, window.ResetsAt)
			if err != nil {
				continue
			}
			reset = lo.ToPtr(parsed)
		}
		ratio := *window.Utilization / 100
		status := "available"
		if ratio >= 1 {
			status = "exhausted"
		} else if ratio >= WarningThresholdRatio {
			status = "warning"
		}
		data.Limits = append(data.Limits, QuotaLimitStatus{
			Type: QuotaLimitTypeToken, Window: entry.window, UsageRatio: ratio,
			Status: status, Ready: IsReadyStatus(status), NextResetAt: reset,
		})
	}
	return NormalizeQuotaData(data), nil
}
