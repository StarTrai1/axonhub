package orchestrator

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer"
)

// applyStrictChatRoles runs after pass-through and overrides so native Chat and
// converted requests use the selected destination's supported instruction role.
func applyStrictChatRoles(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return pipeline.OnRawRequest("strict-chat-roles", func(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		if request == nil || request.APIFormat != string(llm.APIFormatOpenAIChatCompletion) ||
			!requiresSystemChatRole(outbound.GetCurrentChannel(), request.URL) {
			return request, nil
		}

		body := request.Body
		if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
			return nil, fmt.Errorf("%w: invalid Chat request object", transformer.ErrInvalidRequest)
		}
		messages := gjson.GetBytes(body, "messages")
		if !messages.IsArray() {
			return nil, fmt.Errorf("%w: invalid Chat messages array", transformer.ErrInvalidRequest)
		}
		for index, message := range messages.Array() {
			if message.Get("role").String() != "developer" {
				continue
			}
			// Patch only the role. Preserve message order, unknown fields, content
			// parts and large JSON numbers, including native pass-through payloads.
			updated, err := sjson.SetBytes(body, "messages."+strconv.Itoa(index)+".role", "system")
			if err != nil {
				return nil, fmt.Errorf("%w: normalize Chat instruction role: %v", transformer.ErrInvalidRequest, err)
			}
			body = updated
		}
		request.Body = body
		return request, nil
	})
}

func requiresSystemChatRole(selected *biz.Channel, targetURL string) bool {
	if selected != nil && selected.Channel != nil {
		//nolint:exhaustive // Only these provider types require this adaptation.
		switch selected.Type {
		case channel.TypeDeepseek, channel.TypeDeepseekAnthropic,
			channel.TypeMoonshot, channel.TypeMoonshotAnthropic, channel.TypeMoonshotCoding,
			channel.TypeZhipu, channel.TypeZhipuAnthropic, channel.TypeZai, channel.TypeZaiAnthropic:
			return true
		}
	}
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return false
	}
	switch strings.ToLower(parsed.Hostname()) {
	case "api.deepseek.com", "api.kimi.com", "api.moonshot.cn", "api.moonshot.ai", "open.bigmodel.cn", "api.z.ai":
		return true
	default:
		return false
	}
}
