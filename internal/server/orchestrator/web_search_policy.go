package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/samber/lo"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

const responsesLiteHostedWebSearchTools = `[{"type":"web_search","external_web_access":true,"search_content_types":["text","image"]}]`

// applyResponsesLiteWebSearchFallback restores the compatible hosted web search tool that
// Responses Lite intentionally omits from the top-level tools field. It runs
// after body pass-through so the injected tool cannot be overwritten by the
// original inbound payload. Providers that reject the compatibility tool with
// 400/422 get one same-channel retry without this gateway-authored field.
func applyResponsesLiteWebSearchFallback(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return &responsesLiteWebSearchFallbackMiddleware{outbound: outbound}
}

type responsesLiteWebSearchFallbackMiddleware struct {
	pipeline.DummyMiddleware
	outbound *PersistentOutboundTransformer
}

func (m *responsesLiteWebSearchFallbackMiddleware) Name() string {
	return "responses-lite-web-search-fallback"
}

func (m *responsesLiteWebSearchFallbackMiddleware) OnOutboundRawRequest(
	ctx context.Context,
	request *httpclient.Request,
) (*httpclient.Request, error) {
	if m.outbound == nil || m.outbound.state == nil {
		return request, nil
	}
	m.outbound.state.responsesLiteWebSearchInjectedChannel = 0

	channel := m.outbound.GetCurrentChannel()
	if channel == nil || responsesLiteWebSearchBlockedForChannel(m.outbound.state, channel.ID) ||
		!shouldInjectResponsesLiteWebSearch(m.outbound, request) {
		return request, nil
	}

	body, err := sjson.SetRawBytes(request.Body, "tools", []byte(responsesLiteHostedWebSearchTools))
	if err != nil {
		return nil, fmt.Errorf("inject Responses Lite hosted web search tool: %w", err)
	}
	request.Body = body
	m.outbound.state.responsesLiteWebSearchInjectedChannel = channel.ID

	log.Debug(ctx, "injected hosted web search tool for Responses Lite compatibility",
		log.String("channel", channel.Name),
		log.Int("channel_id", channel.ID))

	return request, nil
}

func (m *responsesLiteWebSearchFallbackMiddleware) OnOutboundRawError(ctx context.Context, err error) {
	if m.outbound == nil || m.outbound.state == nil {
		return
	}
	state := m.outbound.state
	channel := m.outbound.GetCurrentChannel()
	if channel == nil || state.responsesLiteWebSearchInjectedChannel != channel.ID {
		return
	}

	statusCode := ExtractStatusCodeFromError(err)
	if statusCode != http.StatusBadRequest && statusCode != http.StatusUnprocessableEntity {
		return
	}
	if state.responsesLiteWebSearchBlockedChannels == nil {
		state.responsesLiteWebSearchBlockedChannels = make(map[int]struct{})
	}
	state.responsesLiteWebSearchBlockedChannels[channel.ID] = struct{}{}
	state.responsesLiteWebSearchRetryChannel = channel.ID

	log.Info(ctx, "hosted web search tool rejected; scheduling compatibility retry without gateway injection",
		log.String("channel", channel.Name),
		log.Int("channel_id", channel.ID),
		log.Int("status_code", statusCode))
}

func responsesLiteWebSearchBlockedForChannel(state *PersistenceState, channelID int) bool {
	if state == nil || state.responsesLiteWebSearchBlockedChannels == nil {
		return false
	}
	_, blocked := state.responsesLiteWebSearchBlockedChannels[channelID]

	return blocked
}

func hasResponsesLiteWebSearchCompatibilityRetry(state *PersistenceState, channelID int) bool {
	return state != nil && channelID > 0 && state.responsesLiteWebSearchRetryChannel == channelID
}

func shouldInjectResponsesLiteWebSearch(outbound *PersistentOutboundTransformer, request *httpclient.Request) bool {
	if outbound == nil || outbound.state == nil || request == nil {
		return false
	}

	candidate := outbound.GetCurrentChannel()
	if candidate == nil || candidate.Type != channel.TypeCodex ||
		candidate.Policies.EffectiveWebSearchPolicy() != objects.WebSearchPolicyAuto {
		return false
	}

	llmRequest := outbound.state.LlmRequest
	if llmRequest == nil || llmRequest.APIFormat != llm.APIFormatOpenAIResponse ||
		llmRequest.RequestType != llm.RequestTypeChat || llmRequest.RawRequest == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(llmRequest.RawRequest.Headers.Get(responses.ResponsesLiteHeader)), "true") {
		return false
	}
	if !responsesLiteRequestNeedsHostedWebSearch(llmRequest.RawRequest.Body) {
		return false
	}

	return !jsonObjectHasKey(request.Body, "tools")
}

func responsesLiteRequestNeedsHostedWebSearch(body []byte) bool {
	var envelope struct {
		Input json.RawMessage `json:"input"`
	}
	if len(body) == 0 || json.Unmarshal(body, &envelope) != nil || jsonObjectHasKey(body, "tools") {
		return false
	}

	var input []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(envelope.Input, &input) != nil {
		return false
	}

	return lo.SomeBy(input, func(item struct {
		Type string `json:"type"`
	},
	) bool {
		return item.Type == "additional_tools"
	})
}

func jsonObjectHasKey(body []byte, key string) bool {
	var object map[string]json.RawMessage
	if len(body) == 0 || json.Unmarshal(body, &object) != nil {
		return false
	}

	_, ok := object[key]

	return ok
}

func applyWebSearchPolicy(req *llm.Request, candidate *biz.Channel) *llm.Request {
	if req == nil || candidate == nil || candidate.Type != channel.TypeCodex || !candidate.Policies.UsesMCPOnlyWebSearch() {
		return req
	}

	filtered := *req
	filtered.Tools = filterStructuredNativeWebSearchTools(req.Tools)
	filtered.ProviderExtensions = llm.CloneProviderExtensions(req.ProviderExtensions)
	removedNativeTool := len(filtered.Tools) != len(req.Tools)

	ext := openAIResponsesRequestExtensions(filtered.ProviderExtensions)
	if ext != nil {
		var removedRawTool bool
		ext.RawTools, ext.ToolSignatures, removedRawTool = filterRawNativeWebSearchTools(
			req.Tools,
			ext.RawTools,
			ext.ToolSignatures,
		)
		removedNativeTool = removedNativeTool || removedRawTool

		for i := range ext.RawInputItems {
			if ext.RawInputItems[i].Type != "additional_tools" {
				continue
			}
			filteredRaw, removed := filterAdditionalTools(ext.RawInputItems[i].Raw)
			if removed {
				ext.RawInputItems[i].Raw = filteredRaw
				removedNativeTool = true
			}
		}

		if isNativeWebSearchToolChoice(ext.RawToolChoice) {
			ext.RawToolChoice = nil
			removedNativeTool = true
		}
	}

	if removedNativeTool && isNativeWebSearchToolChoiceFromLLM(req.ToolChoice) {
		filtered.ToolChoice = &llm.ToolChoice{ToolChoice: lo.ToPtr("auto")}
	}

	return &filtered
}

func openAIResponsesRequestExtensions(ext *llm.ProviderExtensions) *llm.OpenAIResponsesRequestExtensions {
	if ext == nil || ext.OpenAIResponses == nil {
		return nil
	}

	return ext.OpenAIResponses.Request
}

func filterStructuredNativeWebSearchTools(tools []llm.Tool) []llm.Tool {
	if len(tools) == 0 {
		return tools
	}

	filtered := make([]llm.Tool, 0, len(tools))
	for _, tool := range tools {
		if isNativeWebSearchType(tool.Type) {
			continue
		}
		filtered = append(filtered, tool)
	}

	return filtered
}

func filterRawNativeWebSearchTools(
	structuredTools []llm.Tool,
	rawTools []llm.OpenAIResponsesRawFragment,
	toolSignatures []string,
) ([]llm.OpenAIResponsesRawFragment, []string, bool) {
	if len(rawTools) == 0 {
		return rawTools, filterToolSignatures(structuredTools, toolSignatures), false
	}

	rawByIndex := make(map[int]llm.OpenAIResponsesRawFragment, len(rawTools))
	for _, fragment := range rawTools {
		rawByIndex[fragment.OriginalIndex] = fragment
	}

	filteredRaw := make([]llm.OpenAIResponsesRawFragment, 0, len(rawTools))
	filteredSignatures := make([]string, 0, len(toolSignatures))
	structuredIndex := 0
	newIndex := 0
	removed := false
	total := len(structuredTools) + len(rawTools)

	for originalIndex := range total {
		if fragment, ok := rawByIndex[originalIndex]; ok {
			if isNativeWebSearchRawTool(fragment.Raw) || isNativeWebSearchType(fragment.Type) {
				removed = true
				continue
			}
			fragment.OriginalIndex = newIndex
			filteredRaw = append(filteredRaw, fragment)
			newIndex++
			continue
		}

		if structuredIndex >= len(structuredTools) {
			continue
		}
		tool := structuredTools[structuredIndex]
		if !isNativeWebSearchType(tool.Type) {
			if structuredIndex < len(toolSignatures) {
				filteredSignatures = append(filteredSignatures, toolSignatures[structuredIndex])
			}
			newIndex++
		}
		structuredIndex++
	}

	return filteredRaw, filteredSignatures, removed
}

func filterToolSignatures(tools []llm.Tool, signatures []string) []string {
	if len(signatures) == 0 {
		return signatures
	}

	filtered := make([]string, 0, len(signatures))
	for i, tool := range tools {
		if i >= len(signatures) || isNativeWebSearchType(tool.Type) {
			continue
		}
		filtered = append(filtered, signatures[i])
	}

	return filtered
}

func filterAdditionalTools(raw json.RawMessage) (json.RawMessage, bool) {
	var item struct {
		Tools []json.RawMessage `json:"tools"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &item) != nil {
		return raw, false
	}

	filtered := make([]json.RawMessage, 0, len(item.Tools))
	for _, tool := range item.Tools {
		if isNativeWebSearchRawTool(tool) {
			continue
		}
		filtered = append(filtered, tool)
	}
	if len(filtered) == len(item.Tools) {
		return raw, false
	}

	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return raw, false
	}
	toolsRaw, err := json.Marshal(filtered)
	if err != nil {
		return raw, false
	}
	object["tools"] = toolsRaw
	filteredRaw, err := json.Marshal(object)
	if err != nil {
		return raw, false
	}

	return filteredRaw, true
}

func isNativeWebSearchRawTool(raw json.RawMessage) bool {
	var tool struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &tool) != nil {
		return false
	}

	return isNativeWebSearchType(tool.Type) ||
		(strings.EqualFold(tool.Type, "namespace") && strings.EqualFold(tool.Name, "web"))
}

func isNativeWebSearchType(toolType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(toolType)), "web_search")
}

func isNativeWebSearchToolChoice(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}

	var choice struct {
		Type  string `json:"type"`
		Name  string `json:"name"`
		Tools []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"tools"`
	}
	if json.Unmarshal(raw, &choice) != nil {
		return false
	}

	if isNativeWebSearchType(choice.Type) ||
		(strings.EqualFold(choice.Type, "namespace") && strings.EqualFold(choice.Name, "web")) {
		return true
	}

	return lo.SomeBy(choice.Tools, func(tool struct {
		Type string `json:"type"`
		Name string `json:"name"`
	},
	) bool {
		return isNativeWebSearchType(tool.Type) ||
			(strings.EqualFold(tool.Type, "namespace") && strings.EqualFold(tool.Name, "web"))
	})
}

func isNativeWebSearchToolChoiceFromLLM(choice *llm.ToolChoice) bool {
	return choice != nil &&
		choice.NamedToolChoice != nil &&
		isNativeWebSearchType(choice.NamedToolChoice.Type)
}
