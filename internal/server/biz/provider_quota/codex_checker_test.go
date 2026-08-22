package provider_quota

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestCodexQuotaChecker_UsesOfficialUsageHeaders(t *testing.T) {
	accessToken := buildCodexQuotaTestJWT(t, "acct_test")
	requestCount := 0

	httpClient := newCodexQuotaTestHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		require.Equal(t, "axonhub/1.0", req.Header.Get("User-Agent"))
		require.Empty(t, req.Header.Get("Originator"))
		require.Equal(t, "acct_test", req.Header.Get("Chatgpt-Account-Id"))
		require.Equal(t, "Bearer "+accessToken, req.Header.Get("Authorization"))

		switch req.URL.Path {
		case "/backend-api/wham/usage":
			return codexQuotaTestResponse(`{
				"plan_type":"plus",
				"rate_limit":{"allowed":true},
				"rate_limit_reset_credits":{"available_count":2}
			}`), nil
		case "/backend-api/wham/rate-limit-reset-credits":
			return codexQuotaTestResponse(`{
				"credits":[
					{"id":"later","status":"available","reset_type":"codex_rate_limits","expires_at":"2099-08-23T10:00:00Z"},
					{"id":"next","status":"available","reset_type":"codex_rate_limits","expires_at":"2099-08-22T10:00:00Z"}
				],
				"available_count":2
			}`), nil
		default:
			t.Fatalf("unexpected request path: %s", req.URL.Path)
			return nil, nil
		}
	}))

	checker := NewCodexQuotaChecker(httpClient)

	quota, err := checker.CheckQuota(context.Background(), codexQuotaTestChannel(accessToken, ""))
	require.NoError(t, err)
	require.Equal(t, "available", quota.Status)
	require.True(t, quota.Ready)
	require.Equal(t, 2, requestCount)
	resetCredits, ok := quota.RawData["rate_limit_reset_credits"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, 2, resetCredits["available_count"])
	require.Equal(t, "2099-08-22T10:00:00Z", resetCredits["next_expires_at"])

	_, err = checker.CheckQuota(context.Background(), codexQuotaTestChannel(accessToken, ""))
	require.NoError(t, err)
	require.Equal(t, 3, requestCount, "reset-credit details should be served from the short-lived cache")
}

func TestCodexQuotaChecker_CustomRelaySkipsResetCreditEndpoint(t *testing.T) {
	accessToken := buildCodexQuotaTestJWT(t, "acct_relay")
	requestCount := 0
	httpClient := newCodexQuotaTestHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		require.Equal(t, "/backend-api/wham/usage", req.URL.Path)
		return codexQuotaTestResponse(`{"plan_type":"plus","rate_limit":{"allowed":true}}`), nil
	}))

	checker := NewCodexQuotaChecker(httpClient)
	quota, err := checker.CheckQuota(context.Background(), codexQuotaTestChannel(accessToken, "https://relay.example/v1"))

	require.NoError(t, err)
	require.Equal(t, 1, requestCount)
	require.NotContains(t, quota.RawData, "rate_limit_reset_credits_error")
}

func TestCodexQuotaChecker_ZeroEmbeddedResetCreditCountSkipsDetailEndpoint(t *testing.T) {
	accessToken := buildCodexQuotaTestJWT(t, "acct_zero")
	requestCount := 0
	httpClient := newCodexQuotaTestHTTPClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requestCount++
		require.Equal(t, "/backend-api/wham/usage", req.URL.Path)
		return codexQuotaTestResponse(`{
			"rate_limit":{"allowed":true},
			"rate_limit_reset_credits":{"available_count":0}
		}`), nil
	}))

	checker := NewCodexQuotaChecker(httpClient)
	quota, err := checker.CheckQuota(context.Background(), codexQuotaTestChannel(accessToken, ""))

	require.NoError(t, err)
	require.Equal(t, 1, requestCount)
	resetCredits, ok := quota.RawData["rate_limit_reset_credits"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, 0, resetCredits["available_count"])
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

func TestCodexQuotaChecker_CanResetNow_TrueWhenAvailableCredit(t *testing.T) {
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
						{"id": "cred_1", "status": "available", "reset_type": "codex_rate_limits"},
						{"id": "cred_2", "status": "redeemed"}
					],
					"available_count": 1
				}`)),
			}, nil
		}),
	})

	checker := NewCodexQuotaChecker(httpClient)
	canReset, err := checker.CanResetNow(context.Background(), &ent.Channel{
		Credentials: objects.ChannelCredentials{
			OAuth: &objects.OAuthCredentials{AccessToken: accessToken},
		},
	})

	require.NoError(t, err)
	require.True(t, canReset)
}

func TestCodexQuotaChecker_CanResetNow_FalseWhenNoAvailableCredit(t *testing.T) {
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
	canReset, err := checker.CanResetNow(context.Background(), &ent.Channel{
		Credentials: objects.ChannelCredentials{
			OAuth: &objects.OAuthCredentials{AccessToken: accessToken},
		},
	})

	require.NoError(t, err)
	require.False(t, canReset)
}

func TestCodexQuotaChecker_ResetNow_ConsumesFirstAvailableCredit(t *testing.T) {
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
							{"id": "cred_2", "status": "available", "reset_type": "codex_rate_limits"}
						],
						"available_count": 1
					}`)),
				}, nil
			case 2:
				require.Equal(t, "POST", req.Method)
				require.Equal(t, "/backend-api/wham/rate-limit-reset-credits/consume", req.URL.Path)
				require.Equal(t, "acct_reset", req.Header.Get("Chatgpt-Account-Id"))

				body, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Contains(t, string(body), `"credit_id":"cred_2"`)
				require.Contains(t, string(body), `"redeem_request_id":"`)

				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body: io.NopCloser(strings.NewReader(`{
						"code": "reset",
						"windows_reset": 1,
						"credit": {"id": "cred_2", "status": "redeemed", "redeemed_at": "2026-06-13T13:12:31Z"}
					}`)),
				}, nil
			default:
				t.Fatalf("unexpected request: %d", requestCount)
				return nil, nil
			}
		}),
	})

	checker := NewCodexQuotaChecker(httpClient)
	resp, err := checker.ResetNow(context.Background(), &ent.Channel{
		Credentials: objects.ChannelCredentials{
			OAuth: &objects.OAuthCredentials{AccessToken: accessToken},
		},
	})

	require.NoError(t, err)
	require.Equal(t, "reset", resp.Code)
	require.Equal(t, 1, resp.WindowsReset)
	require.Equal(t, "cred_2", resp.Credit.ID)
	require.Equal(t, "redeemed", resp.Credit.Status)
	require.Equal(t, 2, requestCount)
}

func TestCodexQuotaChecker_ResetNow_ReturnsErrorWhenNoAvailableCredit(t *testing.T) {
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
	resp, err := checker.ResetNow(context.Background(), &ent.Channel{
		Credentials: objects.ChannelCredentials{
			OAuth: &objects.OAuthCredentials{AccessToken: accessToken},
		},
	})

	require.Error(t, err)
	require.Contains(t, err.Error(), "no available codex reset credit")
	require.Nil(t, resp)
}
