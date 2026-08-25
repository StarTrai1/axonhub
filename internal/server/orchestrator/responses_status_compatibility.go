package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
)

var (
	responsesRejectedStatusParamPattern   = regexp.MustCompile(`(?i)^input\[(\d+)\]\.status$`)
	responsesRejectedStatusMessagePattern = regexp.MustCompile(
		`(?i)(?:unknown|unsupported)[ _-]+parameter\s*(?::|=|is)?\s*["']?(input\[\d+\]\.status)(?:["']|\b)`,
	)
)

type responsesRejectedStatusRule struct {
	itemType string
	index    int
}

func applyResponsesRejectedStatusCompatibility(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return &responsesRejectedStatusCompatibilityMiddleware{outbound: outbound}
}

type responsesRejectedStatusCompatibilityMiddleware struct {
	pipeline.DummyMiddleware
	outbound *PersistentOutboundTransformer
}

func (m *responsesRejectedStatusCompatibilityMiddleware) Name() string {
	return "responses-rejected-status-compatibility"
}

func (m *responsesRejectedStatusCompatibilityMiddleware) OnOutboundRawRequest(
	ctx context.Context,
	request *httpclient.Request,
) (*httpclient.Request, error) {
	if m.outbound == nil || m.outbound.state == nil || request == nil {
		return request, nil
	}

	channel := m.outbound.GetCurrentChannel()
	if channel == nil || len(m.outbound.state.responsesRejectedStatusRules[channel.ID]) == 0 {
		return request, nil
	}

	body, changed, err := stripResponsesRejectedStatus(request.Body, m.outbound.state.responsesRejectedStatusRules[channel.ID])
	if err != nil {
		return nil, err
	}
	if !changed {
		return request, nil
	}

	request.Body = body
	log.Debug(ctx, "removed upstream-rejected Responses status fields",
		log.Int("channel_id", channel.ID),
		log.String("channel", channel.Name))

	return request, nil
}

func (m *responsesRejectedStatusCompatibilityMiddleware) OnOutboundRawError(ctx context.Context, err error) {
	if m.outbound == nil || m.outbound.state == nil {
		return
	}

	state := m.outbound.state
	channel := m.outbound.GetCurrentChannel()
	if channel == nil || state.RawProviderRequest == nil ||
		state.RawProviderRequest.APIFormat != string(llm.APIFormatOpenAIResponse) {
		return
	}

	rule, ok := responsesRejectedStatusRuleFromError(err, state.RawProviderRequest.Body)
	if !ok || !rememberResponsesRejectedStatusRule(state, channel.ID, rule) {
		return
	}

	state.responsesRejectedStatusRetryChannel = channel.ID
	log.Info(ctx, "Responses input status rejected; scheduling compatible same-channel retry",
		log.Int("channel_id", channel.ID),
		log.String("channel", channel.Name),
		log.String("item_type", rule.itemType),
		log.Int("input_index", rule.index))
}

func responsesRejectedStatusRuleFromError(err error, requestBody []byte) (responsesRejectedStatusRule, bool) {
	var httpErr *httpclient.Error
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusBadRequest || len(httpErr.Body) == 0 {
		return responsesRejectedStatusRule{}, false
	}

	code := strings.ToLower(strings.TrimSpace(gjson.GetBytes(httpErr.Body, "error.code").String()))
	message := strings.TrimSpace(gjson.GetBytes(httpErr.Body, "error.message").String())
	param := strings.ToLower(strings.TrimSpace(gjson.GetBytes(httpErr.Body, "error.param").String()))
	messageParam := responsesRejectedStatusParamFromMessage(message)
	if param != "" && messageParam != "" && param != messageParam {
		return responsesRejectedStatusRule{}, false
	}
	if code != "unknown_parameter" && code != "unsupported_parameter" && messageParam == "" {
		return responsesRejectedStatusRule{}, false
	}
	if param == "" {
		param = messageParam
	}

	match := responsesRejectedStatusParamPattern.FindStringSubmatch(param)
	if len(match) != 2 {
		return responsesRejectedStatusRule{}, false
	}
	index, parseErr := strconv.Atoi(match[1])
	if parseErr != nil || index < 0 {
		return responsesRejectedStatusRule{}, false
	}

	item := gjson.GetBytes(requestBody, fmt.Sprintf("input.%d", index))
	if !item.IsObject() || !item.Get("status").Exists() {
		return responsesRejectedStatusRule{}, false
	}

	return responsesRejectedStatusRule{
		itemType: strings.TrimSpace(item.Get("type").String()),
		index:    index,
	}, true
}

func responsesRejectedStatusParamFromMessage(message string) string {
	match := responsesRejectedStatusMessagePattern.FindStringSubmatch(strings.TrimSpace(message))
	if len(match) != 2 {
		return ""
	}

	return strings.ToLower(strings.TrimSpace(match[1]))
}

func rememberResponsesRejectedStatusRule(state *PersistenceState, channelID int, rule responsesRejectedStatusRule) bool {
	if state.responsesRejectedStatusRules == nil {
		state.responsesRejectedStatusRules = make(map[int][]responsesRejectedStatusRule)
	}
	for _, existing := range state.responsesRejectedStatusRules[channelID] {
		if rule.itemType != "" && existing.itemType == rule.itemType {
			return false
		}
		if rule.itemType == "" && existing.itemType == "" && existing.index == rule.index {
			return false
		}
	}

	state.responsesRejectedStatusRules[channelID] = append(state.responsesRejectedStatusRules[channelID], rule)

	return true
}

func stripResponsesRejectedStatus(body []byte, rules []responsesRejectedStatusRule) ([]byte, bool, error) {
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() || len(rules) == 0 {
		return body, false, nil
	}

	rewritten := body
	changed := false
	for index, item := range input.Array() {
		if !item.IsObject() || !responsesRejectedStatusRuleMatches(rules, index, strings.TrimSpace(item.Get("type").String())) {
			continue
		}

		path := fmt.Sprintf("input.%d.status", index)
		if !gjson.GetBytes(rewritten, path).Exists() {
			continue
		}
		next, err := sjson.DeleteBytes(rewritten, path)
		if err != nil {
			return nil, false, fmt.Errorf("delete rejected Responses status at input[%d]: %w", index, err)
		}
		rewritten = next
		changed = true
	}

	return rewritten, changed, nil
}

func responsesRejectedStatusRuleMatches(rules []responsesRejectedStatusRule, index int, itemType string) bool {
	for _, rule := range rules {
		if rule.itemType != "" && rule.itemType == itemType {
			return true
		}
		if rule.itemType == "" && rule.index == index {
			return true
		}
	}

	return false
}

func hasResponsesRejectedStatusCompatibilityRetry(state *PersistenceState, channelID int) bool {
	return state != nil && channelID > 0 && state.responsesRejectedStatusRetryChannel == channelID
}
