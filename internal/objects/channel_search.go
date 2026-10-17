package objects

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// EffectiveWebSearchPolicy retires automatic Responses emulation. Stored auto
// policies and the old capability flag now use the native search endpoint.
func (p ChannelPolicies) EffectiveWebSearchPolicy() WebSearchPolicy {
	if p.WebSearch == WebSearchPolicyMCPOnly {
		return WebSearchPolicyMCPOnly
	}
	return WebSearchPolicyNative
}

func (p *WebSearchPolicy) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*p = (ChannelPolicies{WebSearch: WebSearchPolicy(value)}).EffectiveWebSearchPolicy()
	return nil
}

func (p WebSearchPolicy) MarshalGQL(w io.Writer) {
	_, _ = fmt.Fprintf(w, "%q", (ChannelPolicies{WebSearch: p}).EffectiveWebSearchPolicy())
}

func (p *WebSearchPolicy) UnmarshalGQL(value any) error {
	text, ok := value.(string)
	if !ok || (text != string(WebSearchPolicyNative) && text != string(WebSearchPolicyMCPOnly)) {
		return fmt.Errorf("web search policy must be native or mcp_only")
	}
	*p = WebSearchPolicy(text)
	return nil
}

var codexSearchModelPattern = regexp.MustCompile(`^gpt-(\d+)(?:\.(\d+))?(?:-[a-z0-9][a-z0-9.-]*)?$`)

// IsCodexSearchModel accepts GPT-5.6 and newer model identifiers, including
// named variants and snapshots. It does not treat arbitrary aliases as models.
func IsCodexSearchModel(model string) bool {
	parts := codexSearchModelPattern.FindStringSubmatch(strings.TrimSpace(model))
	if parts == nil {
		return false
	}
	major, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	minor, _ := strconv.Atoi(parts[2])
	return major > 5 || (major == 5 && minor >= 6)
}

func ValidateCodexSearchModel(settings *ChannelSettings) error {
	if settings == nil {
		return nil
	}
	settings.CodexSearchModel = strings.TrimSpace(settings.CodexSearchModel)
	if settings.CodexSearchModel != "" && !IsCodexSearchModel(settings.CodexSearchModel) {
		return fmt.Errorf("search model must be GPT-5.6 or newer")
	}
	return nil
}
