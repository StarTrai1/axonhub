package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/apikey"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm/httpclient"
)

type apiKeyRefreshContextKey struct{}

type apiKeyRefreshFunc func(context.Context) (context.Context, *httpclient.Error)

// withAPIKeyRefresh captures the authenticated connection identity and resolved
// peer address without retaining the pooled Gin context or mutable headers.
func withAPIKeyRefresh(ctx context.Context, auth *biz.AuthService, original *ent.APIKey, clientIPs []string) context.Context {
	key, keyID, projectID := original.Key, original.ID, original.ProjectID
	noAuth := original.Type == apikey.TypeNoauth
	refresh := apiKeyRefreshFunc(func(requestCtx context.Context) (context.Context, *httpclient.Error) {
		var current *ent.APIKey
		var err error
		if noAuth {
			current, err = auth.AuthenticateNoAuth(requestCtx)
		} else {
			// Do not fall back to no-auth after this connection's key is revoked.
			current, err = auth.AuthenticateAPIKey(requestCtx, key)
		}
		if err != nil {
			if ent.IsNotFound(err) || errors.Is(err, biz.ErrInvalidAPIKey) {
				return nil, apiKeyRefreshError(http.StatusUnauthorized, "Invalid API key")
			}
			log.Error(requestCtx, "Failed to refresh WebSocket API key", log.Cause(err))
			return nil, apiKeyRefreshError(http.StatusInternalServerError, "Failed to validate API key")
		}
		if current.ID != keyID || current.ProjectID != projectID {
			return nil, apiKeyRefreshError(http.StatusUnauthorized, "Authentication scope changed; reconnect required")
		}
		if len(current.AllowedIps) > 0 && !isAnyAllowedIP(clientIPs, current.AllowedIps) {
			return nil, apiKeyRefreshError(http.StatusForbidden, "IP address is not allowed for this API key")
		}
		requestCtx = contexts.WithAPIKey(requestCtx, current)
		requestCtx = contexts.WithProjectID(requestCtx, projectID)
		requestCtx = withSessionScopeForAPIKey(requestCtx, current)
		requestCtx, err = withAPIKeyPrincipal(requestCtx, current)
		if err != nil {
			return nil, apiKeyRefreshError(http.StatusUnauthorized, "Invalid authentication context")
		}
		return requestCtx, nil
	})
	return context.WithValue(ctx, apiKeyRefreshContextKey{}, refresh)
}

// RefreshAPIKeyContext starts an isolated request and revalidates authentication
// when the HTTP authentication middleware installed a persistent-connection hook.
func RefreshAPIKeyContext(ctx context.Context) (context.Context, *httpclient.Error) {
	ctx = contexts.CloneRequestContext(ctx)
	if refresh, ok := ctx.Value(apiKeyRefreshContextKey{}).(apiKeyRefreshFunc); ok {
		return refresh(ctx)
	}
	return ctx, nil
}

func apiKeyRefreshError(status int, message string) *httpclient.Error {
	body, _ := json.Marshal(objects.ErrorResponse{Error: objects.Error{
		Type:    http.StatusText(status),
		Message: message,
	}})
	return &httpclient.Error{StatusCode: status, Status: http.StatusText(status), Body: body}
}
