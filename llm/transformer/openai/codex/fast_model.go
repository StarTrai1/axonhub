package codex

import (
	"strings"

	"github.com/samber/lo"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

// Explicit request tiers and model aliases take precedence over the channel's
// default speed. Relays with both aliases select the matching exact model;
// providers without aliases receive the native service_tier parameter.
func (t *OutboundTransformer) resolveSpeed(model string, serviceTier *string) (string, *string) {
	base, aliasTier, alias := SpeedModel(model)
	if t.preserveSpeedModel && alias && t.defaultServiceTier == "" && (serviceTier == nil || strings.TrimSpace(*serviceTier) == "") {
		return model, serviceTier
	}
	tier := ""
	if serviceTier != nil {
		tier = strings.TrimSpace(*serviceTier)
	}
	if alias && tier == "" {
		if t.speedModels[model] {
			return model, nil
		}
		tier = aliasTier
	}
	if alias {
		model = base
	}
	if tier == "" {
		tier = t.defaultServiceTier
	}
	if tier == "" {
		return model, serviceTier
	}
	if tier == "fast" {
		tier = "priority"
	}
	suffix := ""
	if tier == "priority" {
		suffix = "-fast"
	}
	if tier == "ultrafast" {
		suffix = "-ultrafast"
	}
	if suffix != "" && t.speedModels[model+suffix] {
		return model + suffix, nil
	}
	return model, lo.ToPtr(tier)
}

// normalizeFastModelRequest runs after body pass-through and overrides. These
// aliases are gateway conveniences; the upstream receives the base model and
// service tier. Patch only those fields so native Responses extensions survive.
func (t *OutboundTransformer) normalizeFastModelRequest(request *httpclient.Request) *httpclient.Request {
	if t == nil || request == nil {
		return request
	}
	if !gjson.ValidBytes(request.Body) {
		return request
	}
	model := gjson.GetBytes(request.Body, "model").String()
	if model == "" {
		return request
	}
	oldTier := gjson.GetBytes(request.Body, "service_tier").String()
	baseModel, resolvedTier := t.resolveSpeed(model, lo.ToPtr(oldTier))
	tier := ""
	if resolvedTier != nil {
		tier = *resolvedTier
	}
	if baseModel == model && tier == oldTier {
		return request
	}

	body, err := sjson.SetBytes(request.Body, "model", baseModel)
	if err != nil {
		return request
	}
	if tier == "" {
		body, err = sjson.DeleteBytes(body, "service_tier")
	} else {
		body, err = sjson.SetBytes(body, "service_tier", tier)
	}
	if err != nil {
		return request
	}

	cloned := *request
	cloned.Body = body
	if t.isOfficialCodex() {
		cloned.Headers = request.Headers.Clone()
		if cloned.Headers == nil {
			cloned.Headers = make(map[string][]string)
		}
		cloned.Headers.Set(RoutingHintHeader, "model="+baseModel+";tier="+tier)
	}

	return &cloned
}
