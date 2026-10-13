package httpclient

import (
 "encoding/json"
 "errors"
 "net/http"
 "strings"
)

// Gemini uses 403 for a missing/expired account-bound cache reference as well
// as real authorization failures. Changing credentials cannot restore it.
func IsMissingGeminiCachedContentMessage(status int, message string) bool {
 return status == http.StatusForbidden && strings.Contains(strings.ToLower(message), "cachedcontent not found")
}

func IsMissingGeminiCachedContent(err error) bool {
 var upstream *Error
 if !errors.As(err, &upstream) || upstream.StatusCode != http.StatusForbidden { return false }
 var body struct { Error struct { Message string `json:"message"` } `json:"error"` }
 return json.Unmarshal(upstream.Body, &body) == nil && IsMissingGeminiCachedContentMessage(upstream.StatusCode, body.Error.Message)
}
