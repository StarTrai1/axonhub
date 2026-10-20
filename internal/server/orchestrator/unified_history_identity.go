package orchestrator

import (
	"context"
	"encoding/json"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

type historyProviderIdentity struct {
	ChannelID int    `json:"channel_id"`
	Endpoint  string `json:"endpoint"`
	Format    string `json:"format"`
	Model     string `json:"model"`
	Auth      string `json:"auth"`
}

func historyProviderOrigin(origin shared.ResponsesOrigin) historyProviderIdentity {
	return historyProviderIdentity{ChannelID: origin.ChannelID, Endpoint: origin.Endpoint, Format: origin.Format, Model: origin.Model, Auth: origin.Auth}
}

func (p historyProviderIdentity) origin() shared.ResponsesOrigin {
	return shared.ResponsesOrigin{ChannelID: p.ChannelID, Endpoint: p.Endpoint, Format: p.Format, Model: p.Model, Auth: p.Auth}
}

type nativeHistoryReader struct {
	Provider historyProviderIdentity `json:"provider"`
	OAuth    bool                    `json:"oauth"`
}

type nativeHistoryReaderContextKey struct{}

// Ignore request/trace headers but retain authentication and account boundaries.
// OAuth refresh may rotate a token for the same explicit account. API-key pools
// cannot claim that exception merely by supplying an account header.
func historyReaderCredential(request *httpclient.Request, oauth bool) string {
	account := request.Headers.Get("Chatgpt-Account-Id")
	authKey, authType, authHeader := "", "", ""
	if request.Auth != nil {
		authKey, authType, authHeader = request.Auth.APIKey, request.Auth.Type, request.Auth.HeaderKey
	}
	identity := []any{"credential", request.Headers.Get("Authorization"), authKey, authType, authHeader, account, request.Headers.Get("Openai-Organization"), request.Headers.Get("Openai-Project")}
	if oauth && account != "" {
		identity = []any{"oauth-account", account, request.Headers.Get("Openai-Organization"), request.Headers.Get("Openai-Project")}
	}
	raw, _ := json.Marshal(identity)
	return historyDigest(raw)
}

func nativeHistoryReaderFor(state *PersistenceState) nativeHistoryReader {
	if state == nil || state.RawProviderRequest == nil || state.CurrentCandidate == nil || state.CurrentCandidate.Channel == nil {
		return nativeHistoryReader{}
	}
	request := state.RawProviderRequest
	channel := state.CurrentCandidate.Channel
	oauth := channel.Credentials.IsOAuth()
	return nativeHistoryReader{OAuth: oauth, Provider: historyProviderIdentity{ChannelID: channel.ID, Endpoint: request.URL, Format: request.APIFormat, Model: gjson.GetBytes(request.Body, "model").String(), Auth: historyReaderCredential(request, oauth)}}
}

func withNativeHistoryReader(ctx context.Context, state *PersistenceState) context.Context {
	return context.WithValue(ctx, nativeHistoryReaderContextKey{}, nativeHistoryReaderFor(state))
}
