package subscription

import (
	"net/http"
	"runtime"
)

const (
	AuthorizeURL = "https://auth.x.ai/oauth2/authorize"
	TokenURL     = "https://auth.x.ai/oauth2/token"
	ClientID     = "b1a00492-073a-47ea-816f-4c329264a828"
	RedirectURI  = "http://127.0.0.1:56121/callback"
	Scopes       = "openid profile email offline_access grok-cli:access api:access"
	SSOScopes    = Scopes + " conversations:read conversations:write"

	DefaultBaseURL    = "https://cli-chat-proxy.grok.com/v1"
	BillingWeeklyURL  = DefaultBaseURL + "/billing?format=credits"
	BillingMonthlyURL = DefaultBaseURL + "/billing"

	CLIClientVersion          = "1.0.13"
	CLIClientVersionHeader    = "x-grok-client-version"
	CLIClientIdentifier       = "grok-pager"
	CLIClientIdentifierHeader = "x-grok-client-identifier"
	CLITokenAuth              = "xai-grok-cli"
	CLITokenAuthHeader        = "x-xai-token-auth"

	SSOAccountsURL = "https://accounts.x.ai/"
	SSODeviceURL   = "https://auth.x.ai/oauth2/device/code"
	SSOVerifyURL   = "https://auth.x.ai/oauth2/device/verify"
	SSOApproveURL  = "https://auth.x.ai/oauth2/device/approve"
)

// CLIUserAgent matches the interactive CLI identity on the host platform.
var CLIUserAgent = cliUserAgent(runtime.GOOS, runtime.GOARCH)

func cliUserAgent(platform, arch string) string {
	if platform == "darwin" {
		platform = "macos"
	}
	switch arch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	case "386":
		arch = "x86"
	}
	return "grok-pager/" + CLIClientVersion + " grok-shell/" + CLIClientVersion + " (" + platform + "; " + arch + ")"
}

// ApplyCLIHeaders keeps inference and billing on the same subscription identity.
func ApplyCLIHeaders(headers http.Header) {
	headers.Set(CLITokenAuthHeader, CLITokenAuth)
	headers.Set(CLIClientVersionHeader, CLIClientVersion)
	headers.Set(CLIClientIdentifierHeader, CLIClientIdentifier)
	headers.Set("X-Grok-Client-Mode", "interactive")
	headers.Set("X-Authenticateresponse", "authenticate-response")
	headers.Set("User-Agent", CLIUserAgent)
}

func DefaultModels() []string {
	return []string{
		"grok-4.6",
		"grok-4.5",
		"grok-4.3",
		"grok-3-mini",
		"grok-3-mini-fast",
		"grok-build-0.1",
		"grok-composer-2.5-fast",
		"grok-4.20-0309-reasoning",
		"grok-4.20-0309-non-reasoning",
		"grok-4.20-multi-agent-0309",
	}
}
