package biz

import (
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
)

// AllowsWorkload combines independently selected dedicated workloads. Enabling
// any dedicated scope excludes foreground chat, without clearing other scopes.
func (ch *Channel) AllowsWorkload(req *llm.Request, memory bool) bool {
	if ch == nil || ch.Channel == nil {
		return false
	}
	policy := ch.Policies.EffectiveCodexMemoryPolicy()
	if memory {
		return policy != objects.CodexMemoryExclude
	}
	if policy == objects.CodexMemoryOnly && !ch.IsCodexToolOnly() {
		return false
	}
	return ch.AllowsCodexToolRequest(req)
}

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
	if req != nil && req.RequestType == llm.RequestTypeImage && !ch.AllowsStandaloneImage() {
		return false
	}
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

func (ch *Channel) AllowsStandaloneImage() bool {
	return ch == nil || ch.Channel == nil || ch.Policies.EffectiveImageGenerationPolicy() != objects.ImageGenerationOtherChannels
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
