package oauth

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/looplj/axonhub/llm/httpclient"
)

// RefreshRejectedError means the token endpoint explicitly rejected the refresh
// grant. A new sign-in is required; transport and proxy failures do not qualify.
type RefreshRejectedError struct {
	Code  string
	cause *httpclient.Error
}

func (e *RefreshRejectedError) Error() string {
	return "OAuth refresh rejected (" + e.Code + "); sign in again"
}

func (e *RefreshRejectedError) Unwrap() error { return e.cause }

type rejectedRefresh struct {
	token    [sha256.Size]byte
	clientID string
	err      *RefreshRejectedError
}

func (p *TokenProvider) refreshRejection(creds *OAuthCredentials) error {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if rejected := p.rejectedRefresh; rejected != nil && rejected.clientID == creds.ClientID && rejected.token == sha256.Sum256([]byte(creds.RefreshToken)) {
		return rejected.err
	}
	return nil
}

func (p *TokenProvider) rememberRefreshRejection(creds *OAuthCredentials, err error) *RefreshRejectedError {
	var failure *httpclient.Error
	if !errors.As(err, &failure) || (failure.StatusCode != http.StatusBadRequest && failure.StatusCode != http.StatusUnauthorized) {
		return nil
	}
	var body struct {
		Error json.RawMessage `json:"error"`
		Code  string          `json:"code"`
	}
	if json.Unmarshal(failure.Body, &body) != nil {
		return nil
	}
	var code string
	if json.Unmarshal(body.Error, &code) != nil || code == "" {
		var detail struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(body.Error, &detail)
		code = detail.Code
		if code == "" {
			code = body.Code
		}
	}
	switch code {
	case "invalid_grant", "refresh_token_expired", "refresh_token_reused", "refresh_token_invalidated":
	default:
		return nil
	}
	// Keep status classification without retaining an untrusted response body
	// that may echo credentials. The refusal belongs only to this grant.
	rejected := &RefreshRejectedError{Code: code, cause: &httpclient.Error{StatusCode: failure.StatusCode}}
	rejected.cause.Status = http.StatusText(failure.StatusCode)
	rejected.cause.Body, _ = json.Marshal(map[string]any{"error": map[string]string{
		"code": code, "type": "authentication_error", "message": rejected.Error(),
	}})
	p.mu.Lock()
	if p.creds == creds {
		p.rejectedRefresh = &rejectedRefresh{token: sha256.Sum256([]byte(creds.RefreshToken)), clientID: creds.ClientID, err: rejected}
	}
	p.mu.Unlock()
	slog.Warn("OAuth refresh grant rejected; sign in again", slog.String("code", code))
	return rejected
}
