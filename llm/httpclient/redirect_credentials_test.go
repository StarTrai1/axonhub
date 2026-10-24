package httpclient

import (
 "net/http"
 "net/http/httptest"
 "net/url"
 "testing"

 "github.com/stretchr/testify/require"
)

func TestRedirectCredentialsNeverReturnAfterCrossOrigin(t *testing.T) {
 var original *httptest.Server
 observed := make(chan http.Header, 2)
 other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  observed <- r.Header.Clone()
  http.Redirect(w, r, original.URL+"/return", http.StatusTemporaryRedirect)
 }))
 defer other.Close()
  original = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  if r.URL.Path == "/return" { observed <- r.Header.Clone(); w.WriteHeader(http.StatusNoContent); return }
  http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
 }))
 defer original.Close()
 req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, original.URL, nil)
 require.NoError(t, err)
 for _, name := range []string{"Authorization", "X-Api-Key", "X-Goog-Api-Key", "Cookie", "X-Access-Token", "X-Credential"} { req.Header.Set(name, "fixture") }
 req.Header.Set("X-Request-Id", "trace")
 response, err := NewHttpClient().GetNativeClient().Do(req)
 require.NoError(t, err)
 defer response.Body.Close()
 for _, headers := range []http.Header{<-observed, <-observed} {
  require.Equal(t, "trace", headers.Get("X-Request-Id"))
  for name := range req.Header { if name != "X-Request-Id" { require.Empty(t, headers.Get(name), name) } }
 }
}

func TestRedirectCredentialOriginAndNoncanonicalHeaders(t *testing.T) {
 parse := func(raw string) *url.URL { u, err := url.Parse(raw); require.NoError(t, err); return u }
 for _, tc := range []struct{ target string; same bool }{
  {"https://EXAMPLE.org:443/next", true}, {"https://example.org:444/next", false},
  {"http://example.org/next", false}, {"https://sub.example.org/next", false},
 } {
  t.Run(tc.target, func(t *testing.T) {
   first := &http.Request{URL: parse("https://example.org/start")}
   next := &http.Request{URL: parse(tc.target), Header: http.Header{"x-api-key": {"fixture"}, "Authorization": {"fixture"}, "X-Request-Id": {"trace"}}}
   require.NoError(t, protectRedirectCredentials(next, []*http.Request{first}))
   require.Equal(t, tc.same, next.Header["x-api-key"] != nil)
   require.Equal(t, tc.same, next.Header.Get("Authorization") != "")
   require.Equal(t, "trace", next.Header.Get("X-Request-Id"))
  })
 }
 client := NewHttpClient().WithRejectHTTPSDowngrade()
 require.Error(t, client.GetNativeClient().CheckRedirect(&http.Request{URL: parse("http://example.org")}, []*http.Request{{URL:parse("https://example.org")}}))
}

func TestInjectedRedirectPolicyIsPreserved(t *testing.T) {
 called := false
 original := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
  called = true
  require.Empty(t, req.Header.Get("X-Api-Key"))
  return http.ErrUseLastResponse
 }}
 wrapped := NewHttpClientWithClient(original)
 first, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://first.example", nil)
 require.NoError(t, err)
 next, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://second.example", nil)
 require.NoError(t, err)
 next.Header.Set("X-Api-Key", "fixture")
 require.ErrorIs(t, wrapped.GetNativeClient().CheckRedirect(next, []*http.Request{first}), http.ErrUseLastResponse)
 require.True(t, called)
 require.NotSame(t, original, wrapped.GetNativeClient())
}
