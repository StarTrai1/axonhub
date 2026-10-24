package httpclient

import (
 "fmt"
 "net/http"
 "net/url"
 "strings"
)

// protectRedirectCredentials applies to every hop, including a return to the
// original origin: net/http can copy the first request's headers again.
func protectRedirectCredentials(req *http.Request, via []*http.Request) error {
 if req != nil && len(via) > 0 && via[0] != nil {
  crossed := !sameRedirectOrigin(via[0].URL, req.URL)
  for _, hop := range via[1:] {
   if hop == nil || !sameRedirectOrigin(via[0].URL, hop.URL) { crossed = true }
  }
  if crossed {
   for name := range req.Header {
    if redirectCredentialHeader(name) { delete(req.Header, name) }
   }
  }
 }
 if len(via) >= 10 { return fmt.Errorf("stopped after 10 redirects") }
 return nil
}

func sameRedirectOrigin(a, b *url.URL) bool {
 if a == nil || b == nil || a.Hostname() == "" || b.Hostname() == "" { return false }
 return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Hostname(), b.Hostname()) && redirectOriginPort(a) == redirectOriginPort(b)
}

func redirectOriginPort(u *url.URL) string {
 if port := u.Port(); port != "" { return port }
 switch strings.ToLower(u.Scheme) {
 case "http": return "80"
 case "https": return "443"
 default: return ""
 }
}

func redirectCredentialHeader(name string) bool {
 name = strings.ToLower(name)
 switch name {
 case "authorization", "proxy-authorization", "cookie", "cookie2": return true
 }
 return strings.Contains(name, "api-key") || strings.Contains(name, "apikey") || strings.Contains(name, "api_key") || strings.Contains(name, "token") || strings.Contains(name, "secret") || strings.Contains(name, "credential")
}
