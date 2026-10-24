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
 var observed http.Header
 other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  observed = r.Header.Clone()
  http.Redirect(w, r, original.URL+"/return", http.StatusTemporaryRedirect)
 }))
 defer other.Close()
 var returned http.Header
 original = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  if r.URL.Path == "/return" { returned = r.Header.Clone(); w.WriteHeader(http.StatusNoContent); return }
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
 for _, headers := range []http.Header{observed, returned} {
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
