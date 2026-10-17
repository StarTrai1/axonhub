package biz

import (
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
)

// IsCodexSearchOnly is an opt-in routing restriction, independent of the
// channel's advertised generation models or its Responses transport.
func (ch *Channel) IsCodexSearchOnly() bool {
	return ch != nil && ch.Channel != nil && ch.Type == channel.TypeCodex &&
		ch.Settings != nil && ch.Settings.CodexSearchOnly
}

// AllowsStandaloneSearch keeps explicit opt-out authoritative even for old or
// externally edited configurations that also carry the search-only flag.
func (ch *Channel) AllowsStandaloneSearch() bool {
	if ch == nil || ch.Channel == nil || ch.Type != channel.TypeCodex {
		return true
	}
	if ch.Policies.EffectiveWebSearchPolicy() == objects.WebSearchPolicyOtherChannels {
		return false
	}
	return ch.IsCodexSearchOnly() || !ch.Policies.UsesMCPOnlyWebSearch()
}
