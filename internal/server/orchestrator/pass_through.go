package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/pkg/xcontext"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

const (
	delayedCodexResponsesTerminalGracePeriod = 2 * time.Second
	delayedCodexResponsesUsageTailTimeout    = 6 * time.Minute
	delayedCodexResponsesUpstreamTimeout     = 15 * time.Minute
	delayedCodexResponsesUsagePersistTimeout = 10 * time.Second
)

// codexResponsesPassThroughHeaders contains Codex identity metadata that can
// accompany a pass-through body. Keep this as an explicit allowlist: inbound
// credentials, transport headers, and protocol-selection headers are never copied.
var codexResponsesPassThroughHeaders = []string{
	"X-Codex-Turn-Metadata",
	"X-Codex-Turn-State",
	"X-Codex-Window-Id",
	"X-Client-Request-Id",
	"X-Codex-Beta-Features",
	"Session-Id",
	"Originator",
	"Thread-Id",
}

// isPassThroughEnabled returns true when the effective pass-through flag for the current
// channel is enabled and both the inbound and outbound API formats are identical.
//
// The effective flag is the channel-level PassThroughBody when set, otherwise it falls back
// to the global setting in the request's settings snapshot.
func (p *PersistentOutboundTransformer) isPassThroughEnabled() bool {
	channel := p.GetCurrentChannel()
	if channel == nil {
		return false
	}

	rawReq := p.state.RawProviderRequest
	if rawReq == nil || rawReq.APIFormat == "" {
		return false
	}

	llmReq := p.state.LlmRequest
	if llmReq == nil || string(llmReq.APIFormat) != rawReq.APIFormat {
		return false
	}

	if !passThroughStreamAligned(p.state.OriginalRequestStream, llmReq.Stream) {
		return false
	}

	if channel.Settings != nil && channel.Settings.PassThroughBody != nil {
		return *channel.Settings.PassThroughBody
	}

	return p.state.settings().PassThrough
}

func (p *PersistentOutboundTransformer) isResponsePassThroughEnabled() bool {
	// Native Decisions already retains the raw JSON. Keep its response transform
	// so model restoration and optional gateway cost injection are not bypassed.
	if p.state != nil && p.state.LlmRequest != nil && p.state.LlmRequest.RequestType == llm.RequestTypeDecisions {
		return false
	}
	return p.state != nil && !p.state.DisableResponsePassThrough && p.isPassThroughEnabled()
}

func passThroughStreamAligned(originalStream, effectiveStream *bool) bool {
	originalEnabled := originalStream != nil && *originalStream
	effectiveEnabled := effectiveStream != nil && *effectiveStream

	return originalEnabled == effectiveEnabled
}

func responsesTransportAdaptationEnabled(outbound *PersistentOutboundTransformer) bool {
	if outbound == nil || outbound.state == nil || outbound.state.LlmRequest == nil || outbound.state.RawProviderRequest == nil {
		return false
	}

	source := outbound.state.LlmRequest.APIFormat
	target := llm.APIFormat(outbound.state.RawProviderRequest.APIFormat)
	if source == target || !responses.IsResponsesFormat(source) || !responses.IsResponsesFormat(target) {
		return false
	}

	return true
}

func responsesIdentityPreservationEnabled(outbound *PersistentOutboundTransformer) bool {
	if outbound == nil || outbound.state == nil || outbound.state.DisableResponsePassThrough || outbound.state.LlmRequest == nil || outbound.state.RawProviderRequest == nil {
		return false
	}

	source := outbound.state.LlmRequest.APIFormat
	target := llm.APIFormat(outbound.state.RawProviderRequest.APIFormat)

	return source == target && responses.IsResponsesFormat(source)
}

func responsesResponseTransportAdaptationEnabled(outbound *PersistentOutboundTransformer) bool {
	return outbound != nil && outbound.state != nil && !outbound.state.DisableResponsePassThrough &&
		responsesTransportAdaptationEnabled(outbound)
}

func responsesClientModel(outbound *PersistentOutboundTransformer) string {
	if outbound == nil || outbound.state == nil || outbound.state.LlmRequest == nil {
		return ""
	}

	request := outbound.state.LlmRequest.RawRequest
	if request == nil {
		request = outbound.state.RawRequest
	}
	if request == nil {
		return ""
	}

	body := request.Body
	if len(body) == 0 {
		body = request.JSONBody
	}

	return gjson.GetBytes(body, "model").String()
}

func preserveResponsesModel(body []byte, clientModel string) []byte {
	if clientModel == "" {
		return append([]byte(nil), body...)
	}

	patched := append([]byte(nil), body...)
	for _, path := range []string{"model", "response.model"} {
		if !gjson.GetBytes(patched, path).Exists() {
			continue
		}

		next, err := sjson.SetBytes(patched, path, clientModel)
		if err != nil {
			return append([]byte(nil), body...)
		}
		patched = next
	}

	return patched
}

func applyResponsesTransportRequestBody(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return pipeline.OnRawRequest("responses-transport-request-body", func(ctx context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		if !responsesTransportAdaptationEnabled(outbound) {
			return request, nil
		}

		llmRequest := outbound.state.LlmRequest
		if !outbound.allowPassThroughBody(ctx, llmRequest, request) {
			return request, nil
		}
		source := llmRequest.APIFormat
		target := llm.APIFormat(request.APIFormat)
		body, err := mergePassThroughRequestBodyWithPromptProtection(
			effectiveResponsesRequestBody(outbound.state),
			source,
			llmRequest.Model,
			outbound.state.PromptProtectionMaskRules,
		)
		if err != nil {
			log.Warn(ctx, "failed to merge Responses transport body, keeping outbound body", log.Cause(err))
			return request, nil
		}
		if outbound.state.PromptProtectionBodyCheck != nil {
			if err := outbound.state.PromptProtectionBodyCheck.validate(ctx, body); err != nil {
				log.Warn(ctx, "failed to validate Responses transport body, keeping outbound body", log.Cause(err))
				return request, nil
			}
		}

		body, err = responses.AdaptRequestBody(body, source, target)
		if err != nil {
			return nil, err
		}
		body, err = mergePassThroughRequestBody(body, target, llmRequest.Model)
		if err != nil {
			return nil, err
		}

		cloned := *request
		cloned.Body = body
		cloned.ContentType = "application/json"
		if cloned.Headers == nil {
			cloned.Headers = make(http.Header)
		}
		cloned.Headers.Set("Content-Type", "application/json")
		outbound.state.PassThroughApplied = true

		return &cloned, nil
	})
}

// applyPassThroughRequestBody creates a middleware that reuses the original inbound request body when
// the channel enables pass-through and the inbound and outbound API formats are identical.
// For formats that encode the selected model in the request body, the mapped llmReq.Model is
// written back into the copied raw payload so pass-through does not bypass model mapping.
// Save the actual outbound provider request so pass-through checks use the emitted API format.
func applyPassThroughRequestBody(outbound *PersistentOutboundTransformer, hooks RequestHookProvider) pipeline.Middleware {
	return pipeline.OnRawRequest("pass-through-request-body", func(ctx context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		outbound.state.RawProviderRequest = request

		passThroughEnabled := outbound.isPassThroughEnabled()
		if shouldRepairDelayedCodexResponsesTerminal(outbound) {
			request.DetachedStreamTimeout = delayedCodexResponsesUpstreamTimeout
		}

		if !passThroughEnabled && !responsesTransportAdaptationEnabled(outbound) {
			return request, nil
		}

		channel := outbound.GetCurrentChannel()
		llmReq := outbound.state.LlmRequest
		if !outbound.allowPassThroughBody(ctx, llmReq, request) {
			return request, nil
		}

		// Multipart bodies cannot be reused: the outbound transformer rebuilds the
		// multipart payload with a new boundary in Content-Type, so replaying the inbound
		// bytes would mismatch the header, and form fields cannot be patched via sjson.
		if !passThroughBodySupported(llmReq) {
			return request, nil
		}

		log.Debug(ctx, "applying pass-through body",
			log.String("channel", channel.Name),
			log.String("api_format", request.APIFormat),
		)

		body, err := mergePassThroughRequestBodyWithPromptProtection(
			effectiveResponsesRequestBody(outbound.state),
			llmReq.APIFormat,
			llmReq.Model,
			outbound.state.PromptProtectionMaskRules,
		)
		if err == nil && outbound.state.PromptProtectionBodyCheck != nil {
			err = outbound.state.PromptProtectionBodyCheck.validate(ctx, body)
		}
		if err != nil {
			log.Warn(ctx, "failed to merge pass-through body, keeping outbound body",
				log.String("channel", channel.Name),
				log.Int("channel_id", channel.ID),
				log.Cause(err),
			)

			return request, nil
		}

		request.Body = body

		// The replayed body keeps the inbound media type: sync Content-Type so a JSON
		// image edit is not sent with the multipart header the outbound transformer built.
		if contentType := llmReq.RawRequest.Headers.Get("Content-Type"); contentType != "" {
			if request.Headers == nil {
				request.Headers = make(http.Header)
			}

			request.Headers.Set("Content-Type", contentType)
			request.ContentType = contentType
		}

		outbound.state.PassThroughApplied = true
		if hooks != nil && channel != nil {
			hooks.PassThroughApplied(ctx, channel.ID)
		}

		return request, nil
	})
}

func (p *PersistentOutboundTransformer) allowPassThroughBody(ctx context.Context, llmReq *llm.Request, providerReq *httpclient.Request) bool {
	policy, ok := p.wrapped.(transformer.PassThroughBodyPolicy)
	if !ok {
		return true
	}

	return policy.AllowPassThroughBody(ctx, llmReq, providerReq)
}

func (p *PersistentOutboundTransformer) allowPassThroughResponse(ctx context.Context) bool {
	policy, ok := p.wrapped.(transformer.PassThroughResponsePolicy)
	if !ok {
		return true
	}

	return policy.AllowPassThroughResponse(ctx, p.state.LlmRequest, p.state.RawProviderRequest)
}

// applyPassThroughRequestHeaders forwards Codex identity metadata paired with
// a pass-through body. Protocol-selection headers such as Responses Lite are
// deliberately excluded: the Codex transformer decides whether they apply.
func applyPassThroughRequestHeaders(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return pipeline.OnRawRequest("pass-through-request-headers", func(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		if !outbound.state.PassThroughApplied || outbound.state.LlmRequest == nil ||
			!responses.IsResponsesFormat(outbound.state.LlmRequest.APIFormat) ||
			outbound.state.LlmRequest.RawRequest == nil {
			return request, nil
		}

		if request.Headers == nil {
			request.Headers = make(http.Header)
		}

		inboundHeaders := outbound.state.LlmRequest.RawRequest.Headers
		for _, header := range codexResponsesPassThroughHeaders {
			values := inboundHeaders.Values(header)
			if len(values) == 0 {
				continue
			}

			request.Headers.Del(header)
			for _, value := range values {
				request.Headers.Add(header, value)
			}
		}

		return request, nil
	})
}

// stripUnsupportedCodexPromptCacheOptions runs after pass-through and channel
// body overrides so ChatGPT's private Codex endpoint never receives public
// prompt cache fields those paths may reintroduce.
func stripUnsupportedCodexPromptCacheOptions(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return pipeline.OnRawRequest("strip-codex-prompt-cache-options", func(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		currentChannel := outbound.GetCurrentChannel()
		if currentChannel == nil || currentChannel.Channel == nil || currentChannel.Channel.Type != channel.TypeCodex {
			return request, nil
		}

		hasPromptCacheOptions := gjson.GetBytes(request.Body, "prompt_cache_options").Exists()
		hasPromptCacheBreakpoints := bytes.Contains(request.Body, []byte(`"prompt_cache_breakpoint"`))
		if !hasPromptCacheOptions && !hasPromptCacheBreakpoints {
			return request, nil
		}

		body := request.Body
		if hasPromptCacheOptions {
			var err error
			body, err = sjson.DeleteBytes(body, "prompt_cache_options")
			if err != nil {
				return nil, fmt.Errorf("strip unsupported Codex prompt_cache_options: %w", err)
			}
		}

		if hasPromptCacheBreakpoints {
			inputItems := gjson.GetBytes(body, "input")
			if inputItems.IsArray() {
				for inputIndex, input := range inputItems.Array() {
					paths := make([]string, 0)
					if input.Get("prompt_cache_breakpoint").Exists() {
						paths = append(paths, fmt.Sprintf("input.%d.prompt_cache_breakpoint", inputIndex))
					}
					for _, field := range []string{"content", "output"} {
						parts := input.Get(field)
						if !parts.IsArray() {
							continue
						}
						for partIndex, part := range parts.Array() {
							if part.Get("prompt_cache_breakpoint").Exists() {
								paths = append(paths, fmt.Sprintf("input.%d.%s.%d.prompt_cache_breakpoint", inputIndex, field, partIndex))
							}
						}
					}
					for _, path := range paths {
						var err error
						body, err = sjson.DeleteBytes(body, path)
						if err != nil {
							return nil, fmt.Errorf("strip unsupported Codex %s: %w", path, err)
						}
					}
				}
			}
		}

		request.Body = body

		return request, nil
	})
}

// repairInvalidOpenAIToolSchemas covers same-format pass-through requests that
// bypass the unified tool conversion path. It only parses bodies that contain
// tool schemas and candidate null fields or NUL escapes, keeping the normal hot path allocation-free.
func repairInvalidOpenAIToolSchemas() pipeline.Middleware {
	return pipeline.OnRawRequest("repair-openai-tool-schemas", func(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		switch llm.APIFormat(request.APIFormat) {
		case llm.APIFormatOpenAIChatCompletion, llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseWebSocket, llm.APIFormatAnthropicMessage:
		default:
			return request, nil
		}
		if (!bytes.Contains(request.Body, []byte(`"parameters"`)) && !bytes.Contains(request.Body, []byte(`"input_schema"`))) ||
			(!jsonFieldIsNull(request.Body, []byte(`"parameters"`)) &&
				!jsonFieldIsNull(request.Body, []byte(`"type"`)) &&
				!jsonFieldIsNull(request.Body, []byte(`"properties"`)) &&
				!jsonFieldIsNull(request.Body, []byte(`"required"`)) &&
				!bytes.Contains(request.Body, []byte(`\\0`))) {
			return request, nil
		}

		var payload map[string]any
		decoder := json.NewDecoder(bytes.NewReader(request.Body))
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			return request, nil
		}
		if !repairToolSchemaList(payload["tools"]) {
			return request, nil
		}

		body, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("marshal repaired OpenAI tool schemas: %w", err)
		}
		request.Body = body

		return request, nil
	})
}

func jsonFieldIsNull(body, field []byte) bool {
	for offset := 0; offset < len(body); {
		relative := bytes.Index(body[offset:], field)
		if relative < 0 {
			return false
		}
		index := offset + relative + len(field)
		for index < len(body) && (body[index] == ' ' || body[index] == '\t' || body[index] == '\r' || body[index] == '\n') {
			index++
		}
		if index < len(body) && body[index] == ':' {
			index++
			for index < len(body) && (body[index] == ' ' || body[index] == '\t' || body[index] == '\r' || body[index] == '\n') {
				index++
			}
			if bytes.HasPrefix(body[index:], []byte("null")) {
				return true
			}
		}
		offset += relative + len(field)
	}

	return false
}

func repairToolSchemaList(value any) bool {
	tools, ok := value.([]any)
	if !ok {
		return false
	}

	changed := false
	for _, value := range tools {
		tool, ok := value.(map[string]any)
		if !ok {
			continue
		}

		if tool["type"] == "namespace" && repairToolSchemaList(tool["tools"]) {
			changed = true
		}
		_, hasInputSchema := tool["input_schema"]
		if tool["type"] != "function" && !hasInputSchema {
			continue
		}

		definition := tool
		if nested, ok := tool["function"].(map[string]any); ok {
			definition = nested
		}
		schemaKey := "parameters"
		if _, exists := definition["input_schema"]; exists {
			schemaKey = "input_schema"
		}
		parametersValue, exists := definition[schemaKey]
		if !exists {
			continue
		}

		parameters, ok := parametersValue.(map[string]any)
		if !ok {
			if parametersValue != nil {
				continue
			}
			parameters = map[string]any{}
			definition[schemaKey] = parameters
			changed = true
		}
		if repairInvalidToolSchema(parameters) {
			changed = true
		}
		if typeValue, exists := parameters["type"]; !exists || typeValue == nil {
			parameters["type"] = "object"
			changed = true
		}
		if parameters["type"] == "object" {
			if _, ok := parameters["properties"].(map[string]any); !ok {
				parameters["properties"] = map[string]any{}
				changed = true
			}
		}
	}

	return changed
}

// Only schema-bearing keywords are traversed; examples/default/enum/const and
// property names may themselves contain a literal "required": null.
func repairInvalidToolSchema(schema map[string]any) bool {
	changed := false
	if pattern, ok := schema["pattern"].(string); ok {
		if normalized := normalizeNULPattern(pattern); normalized != pattern {
			schema["pattern"] = normalized
			changed = true
		}
	}
	if value, exists := schema["required"]; exists && value == nil {
		delete(schema, "required")
		changed = true
	}
	for _, key := range []string{"properties", "patternProperties", "$defs", "definitions", "dependentSchemas"} {
		if children, ok := schema[key].(map[string]any); ok {
			for _, value := range children {
				if child, ok := value.(map[string]any); ok && repairInvalidToolSchema(child) {
					changed = true
				}
			}
		}
	}
	for _, key := range []string{"items", "prefixItems", "allOf", "anyOf", "oneOf", "additionalProperties", "additionalItems", "contains", "not", "if", "then", "else", "propertyNames", "unevaluatedProperties", "unevaluatedItems", "contentSchema"} {
		switch value := schema[key].(type) {
		case map[string]any:
			if repairInvalidToolSchema(value) {
				changed = true
			}
		case []any:
			for _, element := range value {
				if child, ok := element.(map[string]any); ok && repairInvalidToolSchema(child) {
					changed = true
				}
			}
		}
	}
	return changed
}

// Strict regex validators reject the legacy \0 spelling. Use the equivalent
// hex escape, retaining the constraint and literal escaped backslashes.
func normalizeNULPattern(pattern string) string {
	var result strings.Builder
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '\\' && i+1 < len(pattern) {
			if pattern[i+1] == '0' && (i+2 == len(pattern) || pattern[i+2] < '0' || pattern[i+2] > '9') {
				result.WriteString(`\x00`)
			} else {
				result.WriteString(pattern[i : i+2])
			}
			i++
			continue
		}
		result.WriteByte(pattern[i])
	}
	return result.String()
}

func mergePassThroughRequestBody(rawBody []byte, apiFormat llm.APIFormat, model string) ([]byte, error) {
	body := append([]byte(nil), rawBody...)

	if !passThroughBodyNeedsModelPatch(apiFormat) {
		return body, nil
	}

	if model == "" {
		return body, nil
	}

	nextBody, err := sjson.SetBytes(body, "model", model)
	if err != nil {
		return nil, fmt.Errorf("set model in pass-through body: %w", err)
	}

	return nextBody, nil
}

// passThroughBodySupported reports whether the raw inbound body can safely replace the
// outbound request body. Multipart bodies cannot be reused: the outbound transformer
// rebuilds the multipart payload with a new boundary in Content-Type, so replaying the
// inbound bytes would mismatch the header, and form fields cannot be patched via sjson.
// JSON image edits are replayable because their model field can be patched in place.
func passThroughBodySupported(llmReq *llm.Request) bool {
	//nolint:exhaustive // only multipart formats are excluded or content-type checked.
	switch llmReq.APIFormat {
	case llm.APIFormatOpenAITranscription,
		llm.APIFormatOpenAITranslation,
		llm.APIFormatOpenAIImageVariation:
		return false
	case llm.APIFormatOpenAIImageEdit:
		// Only the image edit inbound accepts JSON today; for the other formats this
		// branch is unreachable because their inbounds still require multipart.
		if llmReq.RawRequest == nil {
			return false
		}

		mediaType, _, err := mime.ParseMediaType(llmReq.RawRequest.Headers.Get("Content-Type"))
		return err == nil && strings.EqualFold(mediaType, "application/json")
	case llm.APIFormatOpenAIVideo:
		if llmReq.RawRequest == nil {
			return false
		}

		return !strings.HasPrefix(strings.ToLower(llmReq.RawRequest.Headers.Get("Content-Type")), "multipart/")
	default:
		return true
	}
}

func passThroughBodyNeedsModelPatch(apiFormat llm.APIFormat) bool {
	//nolint:exhaustive // other formats do not need a model field.
	switch apiFormat {
	case llm.APIFormatOpenAIChatCompletion,
		llm.APIFormatOpenAICompletion,
		llm.APIFormatOpenAIResponse,
		llm.APIFormatOpenAIResponseWebSocket,
		llm.APIFormatOpenAIResponseCompact,
		llm.APIFormatOpenAISearch,
		llm.APIFormatOpenAIEmbedding,
		llm.APIFormatOpenAIModeration,
		llm.APIFormatOpenAIAlphaSearch,
		llm.APIFormatOpenAIImageGeneration,
		llm.APIFormatOpenAIVideo,
		llm.APIFormatJinaEmbedding,
		llm.APIFormatJinaRerank,
		llm.APIFormatAnthropicMessage,
		// Speech (TTS) has a JSON body with a model field; transcription/translation
		// use multipart bodies that cannot be patched via sjson, so they are excluded.
		llm.APIFormatOpenAISpeech,
		// Image edits submitted as application/json carry a top-level model field.
		// Multipart edit bodies never reach this point (passThroughBodySupported
		// rejects them), so sjson patching only ever runs on JSON payloads.
		llm.APIFormatOpenAIImageEdit,
		llm.APIFormatTypeSafeSystemOne, llm.APIFormatOpenAIDecisions:
		return true
	default:
		return false
	}
}

// applyUserAgentPassThrough creates a middleware that applies the User-Agent pass-through setting.
func applyUserAgentPassThrough(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return pipeline.OnRawRequest("user-agent-pass-through", func(ctx context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		currentChannel := outbound.GetCurrentChannel()
		if currentChannel == nil {
			return request, nil
		}

		passThroughEnabled := outbound.state.settings().UserAgentPassThrough
		if currentChannel.Settings != nil && currentChannel.Settings.PassThroughUserAgent != nil {
			passThroughEnabled = *currentChannel.Settings.PassThroughUserAgent
		}

		// Handle User-Agent header based on pass-through setting
		// This must be done here (before persistRequestExecution) to ensure
		// the correct User-Agent is logged in request execution records.
		if request.Headers == nil {
			request.Headers = make(http.Header)
		}
		channelTestRequest := false
		if outbound.state.LlmRequest != nil && outbound.state.LlmRequest.RawRequest != nil {
			channelTestRequest = outbound.state.LlmRequest.RawRequest.Metadata[channelTestRequestMetadataKey] == "true"
		}
		codexChannel := currentChannel.Channel != nil && currentChannel.Channel.Type == channel.TypeCodex

		if passThroughEnabled || channelTestRequest {
			// Use the original client identity for pass-through and trusted internal channel tests.
			if outbound.state.LlmRequest != nil && outbound.state.LlmRequest.RawRequest != nil {
				if clientUA := outbound.state.LlmRequest.RawRequest.Headers.Get("User-Agent"); clientUA != "" {
					request.Headers.Set("User-Agent", clientUA)
				}
			}
		} else if codexChannel && request.Headers.Get("Originator") == "codex_cli_rs" && request.Headers.Get("Version") != "" {
			// MergeInboundRequest runs before this middleware and may have restored
			// a generic client User-Agent. Keep the synthesized private-endpoint
			// identity coherent when no explicit identity pass-through was requested.
			request.Headers.Set("User-Agent", "codex_cli_rs/"+request.Headers.Get("Version"))
		} else if request.Headers.Get("User-Agent") == "" {
			// Pass-through disabled: use AxonHub's default User-Agent, unless the
			// outbound transformer already set a provider-required one (e.g.
			// GitHubCopilotChat on Copilot channels).
			request.Headers.Set("User-Agent", "axonhub/1.0")
		}

		return request, nil
	})
}

// captureRawProviderResponse stores the raw provider response on state for response pass-through.
func captureRawProviderResponse(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return pipeline.OnRawResponse("capture-raw-provider-response", func(ctx context.Context, response *httpclient.Response) (*httpclient.Response, error) {
		if outbound.isResponsePassThroughEnabled() || responsesResponseTransportAdaptationEnabled(outbound) || responsesIdentityPreservationEnabled(outbound) {
			outbound.state.RawProviderResponse = response
		}

		return response, nil
	})
}

// applyPassThroughResponse replaces the transformed response with the raw provider response
// when PassThroughBody is enabled and the inbound/outbound API formats match, unless the
// outbound transformer vetoes it via transformer.PassThroughResponsePolicy.
func applyPassThroughResponse(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return pipeline.OnInboundRawResponse("pass-through-response", func(ctx context.Context, response *httpclient.Response) (*httpclient.Response, error) {
		if !outbound.isResponsePassThroughEnabled() && !responsesResponseTransportAdaptationEnabled(outbound) && !responsesIdentityPreservationEnabled(outbound) {
			return response, nil
		}

		if !outbound.allowPassThroughResponse(ctx) {
			return response, nil
		}

		rawResp := outbound.state.RawProviderResponse
		if rawResp == nil {
			return response, nil
		}

		log.Debug(ctx, "applying pass-through response",
			log.String("channel", outbound.GetCurrentChannel().Name),
			log.String("api_format", outbound.state.RawProviderRequest.APIFormat),
		)

		if outbound.isResponsePassThroughEnabled() || responsesIdentityPreservationEnabled(outbound) {
			cloned := *rawResp
			if responsesIdentityPreservationEnabled(outbound) {
				cloned.Body = preserveResponsesModel(rawResp.Body, responsesClientModel(outbound))
			}

			return &cloned, nil
		}

		source := outbound.state.LlmRequest.APIFormat
		target := llm.APIFormat(outbound.state.RawProviderRequest.APIFormat)
		body, err := responses.AdaptResponseBody(rawResp.Body, target, source)
		if err != nil {
			return nil, err
		}
		body = preserveResponsesModel(body, responsesClientModel(outbound))
		cloned := *rawResp
		cloned.Body = body
		cloned.Request = response.Request

		return &cloned, nil
	})
}

// captureRawProviderStream records raw events while the transform chain reads.
func captureRawProviderStream(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return captureRawProviderStreamWithTerminalGrace(outbound, delayedCodexResponsesTerminalGracePeriod)
}

func captureRawProviderStreamWithTerminalGrace(outbound *PersistentOutboundTransformer, terminalGracePeriod time.Duration) pipeline.Middleware {
	return pipeline.OnRawStream("capture-raw-provider-stream", func(ctx context.Context, stream streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*httpclient.StreamEvent], error) {
		if shouldRepairDelayedCodexResponsesTerminal(outbound) {
			channel := outbound.GetCurrentChannel()
			stream = maybeRepairDelayedCodexResponsesTerminal(ctx, outbound, stream, terminalGracePeriod,
				newDelayedCodexResponsesUsageRecorder(ctx, outbound.state, channel.Name))
		}
		stream = cacheNativeResponsesSessionStream(ctx, outbound.state, stream)
		if !outbound.isResponsePassThroughEnabled() && !responsesResponseTransportAdaptationEnabled(outbound) && !responsesIdentityPreservationEnabled(outbound) {
			return stream, nil
		}
		recorder := &rawEventRecorder{source: stream, outcome: new(passThroughStreamError), steering: newResponsesSteeringState()}
		outbound.state.PassThroughRecorder = recorder
		return recorder, nil
	})
}

func maybeRepairDelayedCodexResponsesTerminal(
	ctx context.Context,
	outbound *PersistentOutboundTransformer,
	stream streams.Stream[*httpclient.StreamEvent],
	gracePeriod time.Duration,
	onUsage func(*llm.Usage),
) streams.Stream[*httpclient.StreamEvent] {
	if !shouldRepairDelayedCodexResponsesTerminal(outbound) {
		return stream
	}

	channel := outbound.GetCurrentChannel()
	return newDelayedCodexResponsesTerminalStream(
		ctx,
		stream,
		channel.Name,
		gracePeriod,
		delayedCodexResponsesUsageTailTimeout,
		onUsage,
	)
}

func newDelayedCodexResponsesUsageRecorder(
	ctx context.Context,
	state *PersistenceState,
	channelName string,
) func(*llm.Usage) {
	if state == nil || state.UsageLogService == nil || state.Request == nil || state.RequestExec == nil {
		return nil
	}

	usageLogService := state.UsageLogService
	request := state.Request
	requestExec := state.RequestExec

	return func(usage *llm.Usage) {
		if usage == nil {
			return
		}

		persistCtx, cancel := xcontext.DetachWithTimeout(ctx, delayedCodexResponsesUsagePersistTimeout)
		defer cancel()

		if _, err := usageLogService.CreateUsageLogFromRequest(persistCtx, request, requestExec, usage); err != nil {
			log.Warn(persistCtx, "failed to persist delayed Codex response usage",
				log.String("channel", channelName),
				log.Int("request_id", request.ID),
				log.Int("request_execution_id", requestExec.ID),
				log.Cause(err),
			)
		}
	}
}

func shouldRepairDelayedCodexResponsesTerminal(outbound *PersistentOutboundTransformer) bool {
	if outbound == nil || outbound.state == nil {
		return false
	}

	currentChannel := outbound.GetCurrentChannel()
	request := outbound.state.RawProviderRequest
	if currentChannel == nil || currentChannel.Type != channel.TypeCodex || request == nil {
		return false
	}
	if !responses.IsResponsesFormat(llm.APIFormat(request.APIFormat)) {
		return false
	}

	return strings.EqualFold(strings.TrimSpace(request.Headers.Get(responses.ResponsesLiteHeader)), "true")
}

// delayedCodexResponsesTerminalStream repairs Codex-compatible providers that finish a
// deliverable output but hold response.completed for several minutes. It waits for a short
// idle grace after assistant messages and client-executable tool calls. Reasoning-only output
// cannot finish a turn, so it never starts the terminal grace timer.
//
//nolint:containedctx // The stream must observe request and retry cancellation while Next blocks.
type delayedCodexResponsesTerminalStream struct {
	ctx         context.Context
	stream      streams.Stream[*httpclient.StreamEvent]
	channelName string
	gracePeriod time.Duration
	tailTimeout time.Duration
	onUsage     func(*llm.Usage)

	current          *httpclient.StreamEvent
	err              error
	done             bool
	synthesized      bool
	terminalObserved bool
	steeringObserved bool
	response         *responses.Response
	lastSequence     int
	activeOutputs    map[int]struct{}
	completedOutputs map[int]responses.Item
	closeOnce        sync.Once
	closeErr         error
	tailOwned        atomic.Bool
}

func newDelayedCodexResponsesTerminalStream(
	ctx context.Context,
	stream streams.Stream[*httpclient.StreamEvent],
	channelName string,
	gracePeriod time.Duration,
	tailTimeout time.Duration,
	onUsage func(*llm.Usage),
) streams.Stream[*httpclient.StreamEvent] {
	return &delayedCodexResponsesTerminalStream{
		ctx:              ctx,
		stream:           stream,
		channelName:      channelName,
		gracePeriod:      gracePeriod,
		tailTimeout:      tailTimeout,
		onUsage:          onUsage,
		activeOutputs:    make(map[int]struct{}),
		completedOutputs: make(map[int]responses.Item),
	}
}

func (s *delayedCodexResponsesTerminalStream) Next() bool {
	if s.done {
		return false
	}

	next := s.nextResult()

	var timer *time.Timer
	var timerCh <-chan time.Time
	if s.canSynthesizeCompleted() {
		timer = time.NewTimer(s.gracePeriod)
		timerCh = timer.C
	}
	if timer != nil {
		defer timer.Stop()
	}

	select {
	case result := <-next:
		if result.panicValue != nil {
			s.err = fmt.Errorf("read delayed Codex response stream: %v", result.panicValue)
			s.done = true
			_ = s.Close()

			return false
		}
		if !result.ok {
			s.done = true

			return false
		}

		s.current = s.stream.Current()
		s.observe(s.current)

		return true

	case <-timerCh:
		event, err := s.synthesizeCompleted()
		if err != nil {
			s.err = fmt.Errorf("synthesize delayed Codex response.completed: %w", err)
			s.done = true
			_ = s.Close()

			return false
		}

		s.current = event
		s.synthesized = true
		s.done = true
		if s.onUsage == nil {
			_ = s.Close()
		} else {
			s.tailOwned.Store(true)
			s.captureDelayedUsage(next)
		}

		log.Warn(s.ctx, "synthesized delayed Codex response.completed",
			log.String("channel", s.channelName),
			log.String("response_id", s.response.ID),
			log.Duration("idle_grace", s.gracePeriod),
		)

		return true

	case <-s.ctx.Done():
		s.err = s.ctx.Err()
		s.done = true
		_ = s.Close()

		return false
	}
}

type delayedCodexStreamNextResult struct {
	ok         bool
	panicValue any
}

func (s *delayedCodexResponsesTerminalStream) nextResult() <-chan delayedCodexStreamNextResult {
	resultCh := make(chan delayedCodexStreamNextResult, 1)
	go func() {
		defer func() {
			if cause := recover(); cause != nil {
				resultCh <- delayedCodexStreamNextResult{panicValue: cause}
			}
		}()

		resultCh <- delayedCodexStreamNextResult{ok: s.stream.Next()}
	}()

	return resultCh
}

func (s *delayedCodexResponsesTerminalStream) captureDelayedUsage(
	next <-chan delayedCodexStreamNextResult,
) {
	go func() {
		defer func() {
			if cause := recover(); cause != nil {
				log.Warn(s.ctx, "delayed Codex usage capture panicked, recovering",
					log.String("channel", s.channelName),
					log.Any("panic", cause),
				)
			}

			_ = s.closeUnderlying()
			s.tailOwned.Store(false)
		}()

		timer := time.NewTimer(s.tailTimeout)
		defer timer.Stop()

		for {
			select {
			case result := <-next:
				if result.panicValue != nil {
					log.Warn(s.ctx, "failed to read delayed Codex usage terminal",
						log.String("channel", s.channelName),
						log.Any("panic", result.panicValue),
					)

					return
				}
				if !result.ok {
					return
				}

				event := s.stream.Current()
				usage, completed := completedResponsesUsage(event)
				if completed {
					if usage != nil {
						s.onUsage(usage)
						log.Debug(s.ctx, "captured delayed Codex response usage",
							log.String("channel", s.channelName),
							log.Int64("total_tokens", usage.TotalTokens),
						)
					}

					return
				}
				if IsTerminalStreamEvent(event) {
					return
				}

				next = s.nextResult()

			case <-timer.C:
				log.Warn(s.ctx, "timed out waiting for delayed Codex response usage",
					log.String("channel", s.channelName),
					log.Duration("tail_timeout", s.tailTimeout),
				)

				return
			}
		}
	}()
}

func completedResponsesUsage(event *httpclient.StreamEvent) (*llm.Usage, bool) {
	if event == nil || len(event.Data) == 0 {
		return nil, false
	}

	var responseEvent struct {
		Type     responses.StreamEventType `json:"type"`
		Response *struct {
			Usage *responses.Usage `json:"usage"`
		} `json:"response"`
	}
	if err := json.Unmarshal(event.Data, &responseEvent); err != nil ||
		responseEvent.Type != responses.StreamEventTypeResponseCompleted {
		return nil, false
	}
	if responseEvent.Response == nil || responseEvent.Response.Usage == nil {
		return nil, true
	}

	return responseEvent.Response.Usage.ToUsage(), true
}

func (s *delayedCodexResponsesTerminalStream) observe(event *httpclient.StreamEvent) {
	if event == nil || len(event.Data) == 0 {
		return
	}

	// Most Responses Lite events are high-frequency text, reasoning, or tool
	// deltas. Decode only their small envelope here and defer full object
	// decoding to the handful of state events used by terminal repair.
	var responseEvent struct {
		Type           responses.StreamEventType `json:"type"`
		SequenceNumber int                       `json:"sequence_number"`
		OutputIndex    int                       `json:"output_index"`
		Response       json.RawMessage           `json:"response"`
		Item           json.RawMessage           `json:"item"`
	}
	if err := json.Unmarshal(event.Data, &responseEvent); err != nil {
		return
	}

	switch responseEvent.Type {
	case "response.steer.accepted":
		s.steeringObserved = true

	case responses.StreamEventTypeResponseCreated, responses.StreamEventTypeResponseInProgress:
		if len(responseEvent.Response) > 0 {
			var responseSnapshot *responses.Response
			if err := json.Unmarshal(responseEvent.Response, &responseSnapshot); err != nil {
				return
			}
			if responseSnapshot != nil {
				s.response = responseSnapshot
			}
		}

	case responses.StreamEventTypeOutputItemAdded:
		if len(responseEvent.Item) > 0 {
			var item *responses.Item
			if err := json.Unmarshal(responseEvent.Item, &item); err != nil {
				return
			}
		}
		s.activeOutputs[responseEvent.OutputIndex] = struct{}{}

	case responses.StreamEventTypeOutputItemDone:
		var item *responses.Item
		if len(responseEvent.Item) > 0 {
			if err := json.Unmarshal(responseEvent.Item, &item); err != nil {
				return
			}
		}
		delete(s.activeOutputs, responseEvent.OutputIndex)
		if item != nil {
			s.completedOutputs[responseEvent.OutputIndex] = *item
		}

	case responses.StreamEventTypeResponseCompleted,
		responses.StreamEventTypeResponseFailed,
		responses.StreamEventTypeResponseCancelled,
		responses.StreamEventTypeResponseIncomplete,
		responses.StreamEventTypeError:
		var terminal responses.StreamEvent
		if err := json.Unmarshal(event.Data, &terminal); err != nil {
			return
		}
		s.terminalObserved = true
	}

	if responseEvent.SequenceNumber > s.lastSequence {
		s.lastSequence = responseEvent.SequenceNumber
	}
}

func (s *delayedCodexResponsesTerminalStream) canSynthesizeCompleted() bool {
	return !s.terminalObserved &&
		!s.steeringObserved &&
		s.response != nil &&
		s.response.ID != "" &&
		s.hasTurnOutput() &&
		len(s.activeOutputs) == 0
}

// A reasoning item is internal progress, not a completed Codex turn. The client
// needs either assistant output or a client-executable tool call before a relay's
// missing response.completed event can be repaired safely.
func (s *delayedCodexResponsesTerminalStream) hasTurnOutput() bool {
	for _, item := range s.completedOutputs {
		switch item.Type {
		case "message", "function_call", "custom_tool_call":
			return true
		}
	}

	return false
}

func (s *delayedCodexResponsesTerminalStream) synthesizeCompleted() (*httpclient.StreamEvent, error) {
	responseSnapshot := *s.response
	responseSnapshot.Object = "response"
	responseSnapshot.Status = stringPtr("completed")
	responseSnapshot.Error = nil
	responseSnapshot.IncompleteDetails = nil

	indexes := make([]int, 0, len(s.completedOutputs))
	for outputIndex := range s.completedOutputs {
		indexes = append(indexes, outputIndex)
	}
	sort.Ints(indexes)

	responseSnapshot.Output = make([]responses.Item, 0, len(indexes))
	for _, outputIndex := range indexes {
		responseSnapshot.Output = append(responseSnapshot.Output, s.completedOutputs[outputIndex])
	}

	envelope := struct {
		Type           string              `json:"type"`
		SequenceNumber int                 `json:"sequence_number"`
		Response       *responses.Response `json:"response"`
	}{
		Type:           string(responses.StreamEventTypeResponseCompleted),
		SequenceNumber: s.lastSequence + 1,
		Response:       &responseSnapshot,
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}

	return &httpclient.StreamEvent{
		Type: string(responses.StreamEventTypeResponseCompleted),
		Data: data,
	}, nil
}

func (s *delayedCodexResponsesTerminalStream) Current() *httpclient.StreamEvent {
	return s.current
}

func (s *delayedCodexResponsesTerminalStream) Err() error {
	if s.synthesized {
		return nil
	}
	if s.err != nil {
		return s.err
	}

	return s.stream.Err()
}

func (s *delayedCodexResponsesTerminalStream) Close() error {
	if s.tailOwned.Load() {
		return nil
	}

	return s.closeUnderlying()
}

func (s *delayedCodexResponsesTerminalStream) closeUnderlying() error {
	s.closeOnce.Do(func() {
		s.closeErr = s.stream.Close()
	})

	return s.closeErr
}

func stringPtr(value string) *string {
	return &value
}

type passThroughStreamError struct {
	mu       sync.RWMutex
	err      error
	terminal *httpclient.StreamEvent
}

func (e *passThroughStreamError) recordTerminal(event *httpclient.StreamEvent) {
	e.mu.Lock()
	if e.terminal == nil {
		e.terminal = event
	}
	e.mu.Unlock()
}

func (e *passThroughStreamError) loadTerminal() *httpclient.StreamEvent {
	if e == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.terminal
}

// applyPassThroughStream returns the recorded raw provider events instead of the
// re-encoded inbound events. The transformed stream is still consumed event by event,
// so LLM middlewares and persistence observe the attempt as they do without pass-through.
func applyPassThroughStream(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return pipeline.OnInboundRawStream("pass-through-response-stream", func(ctx context.Context, stream streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*httpclient.StreamEvent], error) {
		adaptResponses := responsesResponseTransportAdaptationEnabled(outbound)
		preserveResponses := responsesIdentityPreservationEnabled(outbound)
		if !outbound.isResponsePassThroughEnabled() && !adaptResponses && !preserveResponses {
			return stream, nil
		}

		recorder := outbound.state.PassThroughRecorder
		if recorder == nil {
			return stream, nil
		}

		log.Debug(ctx, "applying pass-through stream",
			log.String("channel", outbound.GetCurrentChannel().Name),
		)

		rawStream := streams.Stream[*httpclient.StreamEvent](&passThroughStream{transformed: stream, recorder: recorder})
		if adaptResponses {
			rawStream = &responsesTransportStream{
				stream: rawStream,
				source: llm.APIFormat(outbound.state.RawProviderRequest.APIFormat),
				target: outbound.state.LlmRequest.APIFormat,
			}
		}
		if adaptResponses || preserveResponses {
			rawStream = &responsesIdentityStream{
				stream:      rawStream,
				clientModel: responsesClientModel(outbound),
			}
		}

		return withPassThroughDelivery(ctx, stream, rawStream), nil
	})
}

// passThroughStream pulls the transformed stream and emits the raw events recorded
// while producing each transformed event. Raw events the transformer drops (for
// example Anthropic ping) are emitted together with the next transformed event.
type passThroughStream struct {
	transformed streams.Stream[*httpclient.StreamEvent]
	recorder    *rawEventRecorder
	pending     []*httpclient.StreamEvent
	current     *httpclient.StreamEvent
	done        bool
}

func (s *passThroughStream) Next() bool {
	for len(s.pending) == 0 {
		if s.done {
			return false
		}

		if s.transformed.Next() {
			// Current drives persistence and terminal tracking of the transformed event.
			_ = s.transformed.Current()
		} else {
			s.done = true
		}

		s.pending = s.recorder.take()
	}

	s.current = s.pending[0]
	s.pending[0] = nil
	s.pending = s.pending[1:]

	return true
}

func (s *passThroughStream) Current() *httpclient.StreamEvent { return s.current }

func (s *passThroughStream) Err() error {
	if err := s.recorder.Err(); err != nil {
		return err
	}

	return s.transformed.Err()
}

// Close closes only the transformed chain: the outbound persistent stream releases the
// raw source after collecting its tail.
func (s *passThroughStream) Close() error { return s.transformed.Close() }

// passThroughDeliveryStream keeps delivery acknowledgement and finalization connected
// to the transformed stream, which owns persistence of the attempt.
type passThroughDeliveryStream struct {
	streams.Stream[*httpclient.StreamEvent]

	observer streams.DeliveryAcknowledger
}

func withPassThroughDelivery(
	ctx context.Context,
	transformed streams.Stream[*httpclient.StreamEvent],
	raw streams.Stream[*httpclient.StreamEvent],
) streams.Stream[*httpclient.StreamEvent] {
	observer, ok := transformed.(streams.DeliveryAcknowledger)
	if !ok {
		if lifecycleOwner := streamLifecycleFromContext(ctx); lifecycleOwner != nil {
			observer = lifecycleOwner
		}
	}
	if observer == nil {
		return raw
	}

	return &passThroughDeliveryStream{Stream: raw, observer: observer}
}

func (s *passThroughDeliveryStream) AcknowledgeDelivery() {
	s.observer.AcknowledgeDelivery()
}

func (s *passThroughDeliveryStream) DeliveryAcknowledged() bool {
	return s.observer.DeliveryAcknowledged()
}

func (s *passThroughDeliveryStream) Deliver(ctx context.Context, write func() (bool, error)) error {
	if boundary, ok := s.observer.(streams.DeliveryBoundary); ok {
		return boundary.Deliver(ctx, write)
	}
	terminal, err := write()
	if err == nil && terminal {
		s.AcknowledgeDelivery()
	}
	return err
}

func (s *passThroughDeliveryStream) WaitForFinalization() {
	if waiter, ok := s.observer.(streams.FinalizationWaiter); ok {
		waiter.WaitForFinalization()
	}
}

type responsesTransportStream struct {
	stream  streams.Stream[*httpclient.StreamEvent]
	source  llm.APIFormat
	target  llm.APIFormat
	current *httpclient.StreamEvent
	err     error
}

type responsesIdentityStream struct {
	stream      streams.Stream[*httpclient.StreamEvent]
	clientModel string
	current     *httpclient.StreamEvent
}

func (s *responsesIdentityStream) Next() bool {
	if !s.stream.Next() {
		return false
	}

	s.current = preserveResponsesStreamEventModel(s.stream.Current(), s.clientModel)
	return true
}

func (s *responsesIdentityStream) Current() *httpclient.StreamEvent { return s.current }
func (s *responsesIdentityStream) Err() error                       { return s.stream.Err() }
func (s *responsesIdentityStream) Close() error                     { return s.stream.Close() }

func preserveResponsesStreamEventModel(event *httpclient.StreamEvent, clientModel string) *httpclient.StreamEvent {
	if event == nil || clientModel == "" {
		return event
	}

	cloned := *event
	cloned.Data = preserveResponsesModel(event.Data, clientModel)
	return &cloned
}

func (s *responsesTransportStream) Next() bool {
	if s.err != nil || !s.stream.Next() {
		return false
	}

	event, err := responses.AdaptStreamEvent(s.stream.Current(), s.source, s.target)
	if err != nil {
		s.err = err
		return false
	}
	s.current = event

	return true
}

func (s *responsesTransportStream) Current() *httpclient.StreamEvent { return s.current }

func (s *responsesTransportStream) Err() error {
	if s.err != nil {
		return s.err
	}
	return s.stream.Err()
}

func (s *responsesTransportStream) Close() error { return s.stream.Close() }

// rawEventRecorder wraps the raw provider stream of one attempt and retains every
// event read by the outbound transformer until passThroughStream takes it.
//
// Before the first take, the pipeline may still be pre-reading the attempt
// (first-event timeout, empty-response detection, retry before the first content
// event), so the retained count and bytes are bounded and exceeding them fails the
// attempt with a retryable pre-commit error. Afterwards only bytes are bounded:
// events stay retained only while the inbound transformer holds back its output.
type rawEventRecorder struct {
	source   streams.Stream[*httpclient.StreamEvent]
	outcome  *passThroughStreamError
	steering *responsesSteeringState
	terminal bool
	current  *httpclient.StreamEvent

	mu        sync.Mutex
	pending   []*httpclient.StreamEvent
	bytes     int
	attached  bool
	err       error
	closeOnce sync.Once
	closeErr  error
}

const (
	maxRawEventBytes      = 32 * 1024 * 1024
	maxRawPreAttachEvents = 1024
	maxRawPreAttachBytes  = 64 * 1024 * 1024
	maxRawQueueBytes      = 128 * 1024 * 1024
)

var errRawPreAttachBudgetExceeded = fmt.Errorf("pass-through pre-attachment raw event budget exceeded: %w", pipeline.ErrPreCommitBufferExceeded)

func (r *rawEventRecorder) Next() bool {
	r.mu.Lock()
	failed := r.err != nil
	r.mu.Unlock()
	if failed || r.terminal || !r.source.Next() {
		return false
	}

	event := r.source.Current()
	if event != nil {
		eventType := gjson.GetBytes(event.Data, "type").String()
		if eventType == "" {
			eventType = event.Type
		}
		// Steering acknowledgements can defer the final terminal to a later turn.
		if r.steering == nil {
			r.steering = newResponsesSteeringState()
		}
		final := r.steering.isFinalTerminal(event)
		r.terminal = (strings.HasPrefix(eventType, "response.") || eventType == "error") && final
		if r.terminal && r.outcome != nil {
			r.outcome.recordTerminal(event)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.retain(event); err != nil {
		r.err = err
		return false
	}
	r.current = event

	return true
}

func (r *rawEventRecorder) retain(event *httpclient.StreamEvent) error {
	size := rawStreamEventSize(event)
	if size > maxRawEventBytes {
		return errRawPreAttachBudgetExceeded
	}
	if r.attached {
		if size > maxRawEventBytes || size > maxRawQueueBytes-r.bytes {
			return errRawPreAttachBudgetExceeded
		}
	} else if len(r.pending) >= maxRawPreAttachEvents || size > maxRawPreAttachBytes-r.bytes {
		return errRawPreAttachBudgetExceeded
	}

	r.bytes += size
	r.pending = append(r.pending, event)

	return nil
}

// take hands every retained event over to the pass-through consumer in read order.
func (r *rawEventRecorder) take() []*httpclient.StreamEvent {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.attached = true
	events := r.pending
	r.pending = nil
	r.bytes = 0

	return events
}

func (r *rawEventRecorder) Current() *httpclient.StreamEvent { return r.current }

func (r *rawEventRecorder) Err() error {
	r.mu.Lock()
	err := r.err
	r.mu.Unlock()
	if err != nil {
		return err
	}

	return r.source.Err()
}

func (r *rawEventRecorder) Close() error {
	r.closeOnce.Do(func() {
		r.closeErr = r.source.Close()
	})

	return r.closeErr
}

func rawStreamEventSize(event *httpclient.StreamEvent) int {
	if event == nil {
		return 0
	}

	return len(event.Data) + len(event.Type) + len(event.LastEventID)
}
