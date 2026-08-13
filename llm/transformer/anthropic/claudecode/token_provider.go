package claudecode

import (
	"github.com/looplj/axonhub/llm/oauth"
)

// NewTokenProvider creates a new OAuth token provider for Claude Code.
// Claude uses JSON format for token exchange instead of form-encoded data.
func NewTokenProvider(params oauth.TokenProviderParams) *oauth.TokenProvider {
	userAgent := currentClaudeCodeUserAgent()
	// Use JSON strategy for Claude Code OAuth
	params.ExchangeStrategy = &oauth.JSONStrategy{UserAgent: userAgent}
	params.OAuthUrls = DefaultTokenURLs

	if params.UserAgent == "" {
		params.UserAgent = userAgent
	}

	return oauth.NewTokenProvider(params)
}

// DefaultTokenURLs are the production Claude OAuth endpoints.
var DefaultTokenURLs = oauth.OAuthUrls{
	AuthorizeUrl: AuthorizeURL,
	TokenUrl:     TokenURL,
}
