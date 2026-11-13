package codex

type fastModelPair struct {
	model string
	alias string
}

var fastModelPairs = []fastModelPair{
	{model: "gpt-5.6-sol", alias: "gpt-5.6-sol-fast"},
	{model: "gpt-5.6-terra", alias: "gpt-5.6-terra-fast"},
	{model: "gpt-5.6-luna", alias: "gpt-5.6-luna-fast"},
	{model: "gpt-6-astra", alias: "gpt-6-astra-fast"},
	{model: "gpt-6.1-sol", alias: "gpt-6.1-sol-fast"},
	{model: "gpt-6.1-terra", alias: "gpt-6.1-terra-fast"},
	{model: "gpt-6-sol", alias: "gpt-6-sol-fast"},
	{model: "gpt-6-luna", alias: "gpt-6-luna-fast"},
}

// DefaultModels returns the static fallback when the official OAuth catalog is unavailable.
func DefaultModels() []string {
	models := []string{
		"gpt-5",
		"gpt-5-codex",
		"gpt-5-codex-mini",
		"gpt-5.1",
		"gpt-5.1-codex",
		"gpt-5.1-codex-mini",
		"gpt-5.1-codex-max",
		"gpt-5.2",
		"gpt-5.2-codex",
		"gpt-5.3-codex",
		"gpt-5.3-codex-spark",
		"gpt-5.4",
		"gpt-5.4-mini",
		"gpt-5.5",
		"gpt-5.6-sol",
		"gpt-5.6-terra",
		"gpt-5.6-luna",
		"gpt-6-astra",
		"gpt-6.1-sol",
		"gpt-6.1-terra",
		"gpt-6-sol",
		"gpt-6-luna",
		"gpt-reserve",
	}
	for _, pair := range fastModelPairs {
		models = append(models, pair.alias)
	}
	return append(append(models, "codex-auto-review"), ImageModels()...)
}

// ImageModels supplements the official conversation catalog, which does not
// enumerate the standalone Images API. It is not an account entitlement probe.
func ImageModels() []string {
	return []string{"gpt-image-2", "gpt-image-2.5-sunburst", "gpt-image-2.5-flare", "gpt-image-2.6-sunburst", "gpt-image-2.6-flare"}
}

func fastModelBase(model string) (string, bool) {
	base, _, ok := SpeedModel(model)
	return base, ok
}

// SpeedModel resolves the gateway's explicit speed aliases. Unknown provider
// model names are never stripped merely because they end in a speed suffix.
func SpeedModel(model string) (base, tier string, ok bool) {
	for _, pair := range fastModelPairs {
		if pair.alias == model {
			return pair.model, "priority", true
		}
	}
	switch model {
	case "gpt-6-astra-ultrafast":
		return "gpt-6-astra", "ultrafast", true
	case "gpt-6.1-sol-ultrafast":
		return "gpt-6.1-sol", "ultrafast", true
	default:
		return "", "", false
	}
}

const (
	AxonHubOriginator  = "axonhub"
	CodexCLIOriginator = "codex_cli_rs"
	AuthorizeURL       = "https://auth.openai.com/oauth/authorize"
	//nolint:gosec // false alert.
	TokenURL    = "https://auth.openai.com/oauth/token"
	ClientID    = "app_EMoamEEZ73f0CkXaXp7hrann"
	RedirectURI = "http://localhost:1455/auth/callback"
	Scopes      = "openid profile email offline_access"

	codexDefaultVersion = "0.167.2"

	// codexDefaultUserAgent is the Codex-client-like User-Agent the transformer
	// presents upstream when it owns the outbound identity (i.e. when the
	// user-agent pass-through switch is off). It mirrors the Codex CLI
	// fingerprint style and tracks codexDefaultVersion so the fabricated
	// identity stays consistent with the Version header. Client UA forwarding
	// belongs solely to the orchestrator's user-agent pass-through middleware,
	// which overwrites this value when the switch is enabled.
	codexDefaultUserAgent = "codex_cli_rs/" + codexDefaultVersion

	// fabricatedBetaFeatures mirrors the X-Codex-Beta-Features value the current
	// Codex CLI sends, used when a non-Codex inbound client omits the header.
	fabricatedBetaFeatures = "remote_compaction_v2"
)
