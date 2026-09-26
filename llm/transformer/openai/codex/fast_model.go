package codex

import (
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

// normalizeFastModelRequest runs after body pass-through and overrides. These
// aliases are gateway conveniences; the upstream receives the base model and
// service tier. Patch only those fields so native Responses extensions survive.
func (t *OutboundTransformer) normalizeFastModelRequest(request *httpclient.Request) *httpclient.Request {
	if t == nil || request == nil {
		return request
	}
	baseModel, ok := fastModelBase(gjson.GetBytes(request.Body, "model").String())
	if !ok || !gjson.ValidBytes(request.Body) {
		return request
	}

	body, err := sjson.SetBytes(request.Body, "model", baseModel)
	if err != nil {
		return request
	}
	tier := strings.TrimSpace(gjson.GetBytes(body, "service_tier").String())
	if tier == "" {
		tier = codexFastServiceTier
		body, err = sjson.SetBytes(body, "service_tier", tier)
		if err != nil {
			return request
		}
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
