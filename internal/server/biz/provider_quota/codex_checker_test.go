package provider_quota

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestCodexQuotaChecker_UsesOfficialUsageHeaders(t *testing.T) {
	accessToken := buildCodexQuotaTestJWT(t, "acct_test")
	httpClient := newCodexQuotaTestHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, "/backend-api/wham/usage", req.URL.Path)
		require.Equal(t, "axonhub/1.0", req.Header.Get("User-Agent"))
		require.Empty(t, req.Header.Get("Originator"))
		require.Equal(t, "acct_test", req.Header.Get("Chatgpt-Account-Id"))
		require.Equal(t, "Bearer "+accessToken, req.Header.Get("Authorization"))

		return codexQuotaTestResponse(`{"plan_type":"plus","rate_limit":{"allowed":true}}`), nil
	}))

	checker := NewCodexQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), codexQuotaTestChannel(accessToken, ""))
	require.NoError(t, err)
	require.Equal(t, "available", quota.Status)
	require.True(t, quota.Ready)
}

func TestCodexQuotaChecker_ParseResponsePreservesAdditionalRateLimits(t *testing.T) {
	checker := NewCodexQuotaChecker(nil)
	quota, err := checker.parseResponse([]byte(`{
		"plan_type":"plus",
		"rate_limit":{"allowed":true},
		"additional_rate_limits":[{
			"limit_name":"GPT-Reserve",
			"metered_feature":"base_model_inference",
			"rate_limit":{
				"allowed":true,
				"limit_reached":false,
				"primary_window":{
					"used_percent":24,
					"reset_at":1788249600,
					"reset_after_seconds":3600,
					"limit_window_seconds":604800
				}
			}
		}]
	}`))

	require.NoError(t, err)
	additional, ok := quota.RawData["additional_rate_limits"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, additional, 1)
	require.Equal(t, "GPT-Reserve", additional[0]["limit_name"])
	require.Equal(t, "base_model_inference", additional[0]["metered_feature"])
	rateLimit, ok := additional[0]["rate_limit"].(map[string]any)
	require.True(t, ok)
	primary, ok := rateLimit["primary_window"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, 24.0, primary["used_percent"])
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newCodexQuotaTestHTTPClient(transport http.RoundTripper) *httpclient.HttpClient {
	client := new(http.Client)
	client.Transport = transport
	return httpclient.NewHttpClientWithClient(client)
}

func codexQuotaTestResponse(body string) *http.Response {
	response := new(http.Response)
	response.StatusCode = http.StatusOK
	response.Header = make(http.Header)
	response.Body = io.NopCloser(strings.NewReader(body))
	return response
}

func codexQuotaTestChannel(accessToken, baseURL string) *ent.Channel {
	channel := new(ent.Channel)
	channel.BaseURL = baseURL
	channel.Credentials.OAuth = new(objects.OAuthCredentials)
	channel.Credentials.OAuth.AccessToken = accessToken
	return channel
}

func buildCodexQuotaTestJWT(t *testing.T, accountID string) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": accountID,
		},
	})

	signed, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	return signed
}

func TestCodexQuotaChecker_ListResets_ReturnsAvailableResets(t *testing.T) {
	accessToken := buildCodexQuotaTestJWT(t, "acct_reset")

	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, "GET", req.Method)
			require.Equal(t, "/backend-api/wham/rate-limit-reset-credits", req.URL.Path)
			require.Equal(t, "Bearer "+accessToken, req.Header.Get("Authorization"))
			require.Equal(t, "acct_reset", req.Header.Get("Chatgpt-Account-Id"))

			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"credits": [
						{"id": "cred_1", "status": "available", "reset_type": "codex_rate_limits", "granted_at": "2026-09-01T00:00:00Z", "expires_at": "2026-09-08T00:00:00Z", "title": "Full reset", "description": "Ready to redeem"},
						{"id": "cred_2", "status": "redeemed"}
					],
					"available_count": 1
				}`)),
			}, nil
		}),
	})

	checker := NewCodexQuotaChecker(httpClient)
	resets, err := checker.ListResets(context.Background(), &ent.Channel{
		Credentials: objects.ChannelCredentials{
			OAuth: &objects.OAuthCredentials{AccessToken: accessToken},
		},
	})

	require.NoError(t, err)
	require.True(t, resets.Supported)
	require.Equal(t, 1, resets.AvailableCount)
	require.Len(t, resets.Resets, 1)
	require.Equal(t, "cred_1", resets.Resets[0].ID)
	require.Equal(t, "available", resets.Resets[0].Status)
	require.Equal(t, "codex_rate_limits", resets.Resets[0].Type)
	require.Equal(t, "2026-09-01T00:00:00Z", resets.Resets[0].GrantedAt.Format(time.RFC3339))
	require.Equal(t, "2026-09-08T00:00:00Z", resets.Resets[0].ExpiresAt.Format(time.RFC3339))
	require.Equal(t, "Full reset", resets.Resets[0].Title)
	require.Equal(t, "Ready to redeem", resets.Resets[0].Description)
}

func TestCodexQuotaChecker_ListResets_ReturnsEmptyAvailability(t *testing.T) {
	accessToken := buildCodexQuotaTestJWT(t, "acct_reset")

	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"credits": [
						{"id": "cred_1", "status": "redeemed"}
					],
					"available_count": 0
				}`)),
			}, nil
		}),
	})

	checker := NewCodexQuotaChecker(httpClient)
	resets, err := checker.ListResets(context.Background(), &ent.Channel{
		Credentials: objects.ChannelCredentials{
			OAuth: &objects.OAuthCredentials{AccessToken: accessToken},
		},
	})

	require.NoError(t, err)
	require.True(t, resets.Supported)
	require.Empty(t, resets.Resets)
}

func TestCodexQuotaChecker_Reset_ConsumesSelectedReset(t *testing.T) {
	accessToken := buildCodexQuotaTestJWT(t, "acct_reset")
	requestCount := 0

	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requestCount++
			switch requestCount {
			case 1:
				require.Equal(t, "GET", req.Method)
				require.Equal(t, "/backend-api/wham/rate-limit-reset-credits", req.URL.Path)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
						"credits": [
							{"id": "cred_1", "status": "redeemed"},
							{"id": "cred_2", "status": "available", "reset_type": "codex_rate_limits"},
							{"id": "cred_3", "status": "available", "reset_type": "codex_rate_limits"}
						],
						"available_count": 2
					}`)),
				}, nil
			case 2:
				require.Equal(t, "POST", req.Method)
				require.Equal(t, "/backend-api/wham/rate-limit-reset-credits/consume", req.URL.Path)
				require.Equal(t, "acct_reset", req.Header.Get("Chatgpt-Account-Id"))

				body, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Contains(t, string(body), `"credit_id":"cred_3"`)
				require.Contains(t, string(body), `"redeem_request_id":"`)

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
						"code": "reset",
						"windows_reset": 1,
						"credit": {"id": "cred_3", "status": "redeemed", "redeemed_at": "2026-06-13T13:12:31Z"}
					}`)),
				}, nil
			default:
				t.Fatalf("unexpected request: %d", requestCount)
				return nil, nil
			}
		}),
	})

	checker := NewCodexQuotaChecker(httpClient)
	err := checker.Reset(context.Background(), &ent.Channel{
		Credentials: objects.ChannelCredentials{
			OAuth: &objects.OAuthCredentials{AccessToken: accessToken},
		},
	}, "cred_3")

	require.NoError(t, err)
	require.Equal(t, 2, requestCount)
}

func TestCodexQuotaChecker_Reset_ReturnsErrorWhenNoAvailableReset(t *testing.T) {
	accessToken := buildCodexQuotaTestJWT(t, "acct_reset")

	httpClient := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body: io.NopCloser(strings.NewReader(`{
					"credits": [{"id": "cred_1", "status": "redeemed"}],
					"available_count": 0
				}`)),
			}, nil
		}),
	})

	checker := NewCodexQuotaChecker(httpClient)
	err := checker.Reset(context.Background(), &ent.Channel{
		Credentials: objects.ChannelCredentials{
			OAuth: &objects.OAuthCredentials{AccessToken: accessToken},
		},
	}, "")

	require.Error(t, err)
	require.Contains(t, err.Error(), "no available codex reset credit")
}
