package biz

import (
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
)

// IsCodexSearchOnly is an opt-in routing restriction, independent of the
// channel's advertised generation models or its Responses transport.
func (ch *Channel) IsCodexSearchOnly() bool {
	return ch != nil && ch.Channel != nil && ch.Type == channel.TypeCodex &&
		ch.Settings != nil && ch.Settings.CodexSearchOnly
}

func (ch *Channel) IsCodexToolOnly() bool {
	return ch != nil && ch.Channel != nil && ch.Type == channel.TypeCodex &&
		ch.Settings != nil && (ch.Settings.CodexSearchOnly || ch.Settings.CodexImageOnly)
}

// AllowsCodexToolRequest is shared by selection and final dispatch, so affinity
// and continuation candidates cannot bypass a tool-only channel's restriction.
func (ch *Channel) AllowsCodexToolRequest(req *llm.Request) bool {
	if !ch.IsCodexToolOnly() {
		return true
	}
	if req == nil {
		return false
	}
	if req.RequestType == llm.RequestTypeSearch && req.APIFormat == llm.APIFormatOpenAISearch {
		return ch.Settings.CodexSearchOnly && ch.AllowsStandaloneSearch()
	}
	return ch.Settings.CodexImageOnly && req.RequestType == llm.RequestTypeImage &&
		(req.APIFormat == llm.APIFormatOpenAIImageGeneration || req.APIFormat == llm.APIFormatOpenAIImageEdit)
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
	if ch.IsCodexToolOnly() {
		return ch.Settings.CodexSearchOnly
	}
	return ch.IsCodexSearchOnly() || !ch.Policies.UsesMCPOnlyWebSearch()
}
