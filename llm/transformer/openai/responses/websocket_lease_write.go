package responses

import (
	"context"
	"encoding/json"
 "net/http"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func ContinuationAuthIdentity(request *httpclient.Request) string {
	headers := request.Headers
	return hashSecret(authPoolIdentity(request.Auth, headers) + "\x00" + headerPoolIdentity(headers) + "\x00" + headers.Get(webSocketAccountIDHeader) + "\x00" + headers.Get(http.CanonicalHeaderKey(webSocketOrgHeader)) + "\x00" + headers.Get(webSocketProjectHeader))
}

func isWebSocketRecoveryEvent(raw []byte) bool {
	var event struct {
		Code  string `json:"code"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	return json.Unmarshal(raw, &event) == nil && (event.Code == "previous_response_not_found" || event.Error.Code == "previous_response_not_found")
}

func (l *webSocketLease) write(ctx context.Context, payload map[string]any) error {
	if l.pooled != nil {
		l.pooled.mu.Lock()
		defer l.pooled.mu.Unlock()
		if l.pooled.closed || l.pooled.generation != l.generation {
			return shared.ResponsesRecoveryError()
		}
		if id := explicitPreviousResponseID(payload); id != "" && id != l.pooled.lastResponseID {
			return shared.ResponsesRecoveryError()
		}
	}
	return l.writeJSONContext(ctx, payload)
}
