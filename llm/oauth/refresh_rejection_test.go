package oauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestTokenProvider_RefreshRejection(t *testing.T) {
	for _, tt := range []struct {
		name string
		status int
		body string
		permanent bool
	}{
		{"invalid grant", 400, `{"error":"invalid_grant"}`, true},
		{"expired", 401, `{"error":{"code":"refresh_token_expired"}}`, true},
		{"reused", 400, `{"code":"refresh_token_reused"}`, true},
		{"invalidated", 401, `{"error":"refresh_token_invalidated"}`, true},
		{"proxy unauthorized", 401, `<html>Unauthorized</html>`, false},
		{"unknown unauthorized", 401, `{"error":"unknown"}`, false},
		{"quoted code", 400, `{"error":{"message":"invalid_grant"}}`, false},
		{"temporary", 400, `{"error":"temporarily_unavailable"}`, false},
		{"forbidden", 403, `{"error":"invalid_grant"}`, false},
		{"rate limit", 429, `{"error":"invalid_grant"}`, false},
		{"server failure", 500, `{"error":"invalid_grant"}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(server.Close)
			p := NewTokenProvider(TokenProviderParams{
				Credentials: &OAuthCredentials{ClientID: "client", AccessToken: "old", RefreshToken: "refresh", ExpiresAt: time.Now().Add(-time.Hour)},
				HTTPClient: httpclient.NewHttpClientWithClient(server.Client()), OAuthUrls: OAuthUrls{TokenUrl: server.URL},
			})
			for range 2 {
				_, err := p.Get(t.Context())
				require.Error(t, err)
				var rejected *RefreshRejectedError
				require.Equal(t, tt.permanent, errors.As(err, &rejected))
				var httpErr *httpclient.Error
				require.ErrorAs(t, err, &httpErr)
				require.Equal(t, tt.status, httpErr.StatusCode)
			}
			failed, running := p.runAutoRefreshOnce(context.Background(), time.Minute)
			require.True(t, failed)
			require.True(t, running)
			want := int32(3)
			if tt.permanent { want = 1 }
			require.Equal(t, want, calls.Load())
		})
	}
}

func TestTokenProvider_RefreshRejectionClearsAfterExchange(t *testing.T) {
	var refreshCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := r.ParseForm(); err != nil { t.Error(err); w.WriteHeader(400); return }
		if r.Form.Get("grant_type") == "authorization_code" {
			_, _ = w.Write([]byte(`{"access_token":"new","refresh_token":"replacement","expires_in":1}`))
			return
		}
		refreshCalls.Add(1)
		if r.Form.Get("refresh_token") == "old-refresh" {
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"fresh","expires_in":3600}`))
	}))
	t.Cleanup(server.Close)
	p := NewTokenProvider(TokenProviderParams{
		Credentials: &OAuthCredentials{ClientID: "client", AccessToken: "old", RefreshToken: "old-refresh", ExpiresAt: time.Now().Add(-time.Hour)},
		HTTPClient: httpclient.NewHttpClientWithClient(server.Client()), OAuthUrls: OAuthUrls{TokenUrl: server.URL},
	})
	_, err := p.Get(t.Context())
	require.Error(t, err)
	_, err = p.Exchange(t.Context(), ExchangeParams{Code: "code", CodeVerifier: "verifier", ClientID: "client", RedirectURI: "http://localhost/callback"})
	require.NoError(t, err)
	fresh, err := p.EnsureFresh(t.Context(), time.Minute)
	require.NoError(t, err)
	require.Equal(t, "fresh", fresh.AccessToken)
	require.Equal(t, "replacement", fresh.RefreshToken)
	require.Equal(t, int32(2), refreshCalls.Load())
}

func TestTokenProvider_RefreshRejectionDoesNotPoisonReplacement(t *testing.T) {
	old := &OAuthCredentials{ClientID: "client", RefreshToken: "old"}
	replacement := &OAuthCredentials{ClientID: "client", RefreshToken: "new"}
	p := NewTokenProvider(TokenProviderParams{Credentials: old})
	failure := &httpclient.Error{StatusCode: 400, Body: []byte(`{"error":"invalid_grant"}`)}
	require.NotNil(t, p.rememberRefreshRejection(old, failure))
	require.Error(t, p.refreshRejection(old))
	require.NoError(t, p.refreshRejection(replacement))
	require.NoError(t, p.refreshRejection(&OAuthCredentials{ClientID: "other", RefreshToken: "old"}))
	p.mu.Lock()
	p.creds = replacement
	p.rejectedRefresh = nil
	p.mu.Unlock()
	// An in-flight refusal for the previous grant must not mark a new login.
	require.NotNil(t, p.rememberRefreshRejection(old, failure))
	require.Nil(t, p.rejectedRefresh)
}
