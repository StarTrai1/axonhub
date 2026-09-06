package orchestrator

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/samber/lo"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
)

var (
	responsesRejectedStatusParamPattern   = regexp.MustCompile(`(?i)^input\[(\d+)\]\.(status|internal_chat_message_metadata_passthrough)(?:\.[a-z_]+)?$`)
	responsesRejectedStatusMessagePattern = regexp.MustCompile(
		`(?i)(?:unknown|unsupported)[ _-]+parameter\s*(?::|=|is)?\s*["']?(input\[\d+\]\.status)(?:["']|\b)`,
	)
)

type responsesRejectedStatusRule struct {
	itemType string
	index    int
	field    string
}

type responsesMetadataCapabilityKey struct {
	channelID  int
	url        string
	model      string
	credential [sha256.Size]byte
}

var rejectedResponsesMetadata = lo.Must(lru.New[responsesMetadataCapabilityKey, time.Time](1024))

func responsesMetadataKey(channelID int, request *httpclient.Request) responsesMetadataCapabilityKey {
	credential := request.Headers.Get("Authorization")
	if request.Auth != nil {
		credential += "\x00" + request.Auth.APIKey
	}
	return responsesMetadataCapabilityKey{
		channelID:  channelID,
		url:        request.URL,
		model:      gjson.GetBytes(request.Body, "model").String(),
		credential: sha256.Sum256([]byte(credential)),
	}
}

func (r responsesRejectedStatusRule) fieldName() string {
	if r.field != "" {
		return r.field
	}
	return "status"
}

func applyResponsesRejectedStatusCompatibility(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return &responsesRejectedStatusCompatibilityMiddleware{
		DummyMiddleware: pipeline.DummyMiddleware{},
		outbound:        outbound,
	}
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
	if channel == nil {
		return request, nil
	}
	if request.URL != "" && request.APIFormat == string(llm.APIFormatOpenAIResponse) {
		if expires, ok := rejectedResponsesMetadata.Get(responsesMetadataKey(channel.ID, request)); ok && time.Now().Before(expires) {
			rememberResponsesRejectedStatusRule(m.outbound.state, channel.ID, responsesRejectedStatusRule{
				itemType: "message", field: "internal_chat_message_metadata_passthrough",
			})
		}
	}

	body, changed, err := stripResponsesRejectedStatus(request.Body, m.outbound.state.responsesRejectedStatusRules[channel.ID])
	if err != nil {
		return nil, err
	}
	if !changed {
		return request, nil
	}

	request.Body = body
	log.Debug(ctx, "removed upstream-rejected Responses metadata fields",
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
	if rule.fieldName() == "internal_chat_message_metadata_passthrough" && state.RawProviderRequest.URL != "" {
		rejectedResponsesMetadata.Add(responsesMetadataKey(channel.ID, state.RawProviderRequest), time.Now().Add(30*time.Minute))
	}
	log.Info(ctx, "Responses input metadata rejected; scheduling compatible same-channel retry",
		log.Int("channel_id", channel.ID),
		log.String("channel", channel.Name),
		log.String("item_type", rule.itemType),
		log.String("field", rule.fieldName()),
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
	if len(match) != 3 {
		return responsesRejectedStatusRule{}, false
	}
	index, parseErr := strconv.Atoi(match[1])
	if parseErr != nil || index < 0 {
		return responsesRejectedStatusRule{}, false
	}

	item := gjson.GetBytes(requestBody, fmt.Sprintf("input.%d", index))
	field := match[2]
	if !item.IsObject() || !item.Get(field).Exists() ||
		(field == "status" && !strings.HasSuffix(param, ".status")) ||
		(field == "internal_chat_message_metadata_passthrough" && item.Get("type").String() != "message") {
		return responsesRejectedStatusRule{}, false
	}

	return responsesRejectedStatusRule{
		itemType: strings.TrimSpace(item.Get("type").String()),
		index:    index,
		field:    field,
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
		if existing.fieldName() != rule.fieldName() {
			continue
		}
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
		if !item.IsObject() {
			continue
		}
		for _, rule := range rules {
			if !responsesRejectedStatusRuleMatches([]responsesRejectedStatusRule{rule}, index, strings.TrimSpace(item.Get("type").String())) {
				continue
			}
			path := fmt.Sprintf("input.%d.%s", index, rule.fieldName())
			if !gjson.GetBytes(rewritten, path).Exists() {
				continue
			}
			next, err := sjson.DeleteBytes(rewritten, path)
			if err != nil {
				return nil, false, fmt.Errorf("delete rejected Responses metadata at input[%d]: %w", index, err)
			}
			rewritten = next
			changed = true
		}
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
