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
	"github.com/looplj/axonhub/internal/server/biz"
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
// to the global system setting. systemService may be nil; in that case only the channel-level
// setting is consulted (used by tests that exercise per-channel behavior in isolation).
func (p *PersistentOutboundTransformer) isPassThroughEnabled(ctx context.Context, systemService *biz.SystemService) bool {
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

	var enabled bool

	switch {
	case channel.Settings != nil && channel.Settings.PassThroughBody != nil:
		enabled = *channel.Settings.PassThroughBody
	case systemService != nil:
		global, err := systemService.PassThrough(ctx)
		if err != nil {
			log.Warn(ctx, "failed to get global pass-through setting", log.Cause(err))

			return false
		}

		enabled = global
	}

	return enabled
}

func (p *PersistentOutboundTransformer) isResponsePassThroughEnabled(ctx context.Context, systemService *biz.SystemService) bool {
	return p.state != nil && !p.state.DisableResponsePassThrough && p.isPassThroughEnabled(ctx, systemService)
}

func passThroughStreamAligned(originalStream, effectiveStream *bool) bool {
	originalEnabled := originalStream != nil && *originalStream
	effectiveEnabled := effectiveStream != nil && *effectiveStream

	return originalEnabled == effectiveEnabled
}

// applyPassThroughRequestBody creates a middleware that reuses the original inbound request body when
// the channel enables pass-through and the inbound and outbound API formats are identical.
// For formats that encode the selected model in the request body, the mapped llmReq.Model is
// written back into the copied raw payload so pass-through does not bypass model mapping.
// Save the actual outbound provider request so pass-through checks use the emitted API format.
func applyPassThroughRequestBody(outbound *PersistentOutboundTransformer, systemService *biz.SystemService) pipeline.Middleware {
	return pipeline.OnRawRequest("pass-through-request-body", func(ctx context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		outbound.state.RawProviderRequest = request

		passThroughEnabled := outbound.isPassThroughEnabled(ctx, systemService)
		if shouldRepairDelayedCodexResponsesTerminal(outbound) {
			request.DetachedStreamTimeout = delayedCodexResponsesUpstreamTimeout
		}

		if !passThroughEnabled {
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

		body, err := mergePassThroughRequestBody(llmReq.RawRequest.Body, llmReq.APIFormat, llmReq.Model)
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

// applyPassThroughRequestHeaders forwards Codex identity metadata paired with
// a pass-through body. Protocol-selection headers such as Responses Lite are
// deliberately excluded: the Codex transformer decides whether they apply.
func applyPassThroughRequestHeaders(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return pipeline.OnRawRequest("pass-through-request-headers", func(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		if !outbound.state.PassThroughApplied || outbound.state.LlmRequest == nil ||
			outbound.state.LlmRequest.APIFormat != llm.APIFormatOpenAIResponse ||
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
			for inputIndex, input := range gjson.GetBytes(body, "input").Array() {
				for contentIndex, content := range input.Get("content").Array() {
					if !content.Get("prompt_cache_breakpoint").Exists() {
						continue
					}

					path := fmt.Sprintf("input.%d.content.%d.prompt_cache_breakpoint", inputIndex, contentIndex)
					var err error
					body, err = sjson.DeleteBytes(body, path)
					if err != nil {
						return nil, fmt.Errorf("strip unsupported Codex %s: %w", path, err)
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
// both tool parameters and null, keeping the normal hot path allocation-free.
func repairInvalidOpenAIToolSchemas() pipeline.Middleware {
	return pipeline.OnRawRequest("repair-openai-tool-schemas", func(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		switch llm.APIFormat(request.APIFormat) {
		case llm.APIFormatOpenAIChatCompletion, llm.APIFormatOpenAIResponse:
		default:
			return request, nil
		}
		if !bytes.Contains(request.Body, []byte(`"parameters"`)) ||
			(!jsonFieldIsNull(request.Body, []byte(`"parameters"`)) &&
				!jsonFieldIsNull(request.Body, []byte(`"type"`)) &&
				!jsonFieldIsNull(request.Body, []byte(`"properties"`))) {
			return request, nil
		}

		var payload map[string]any
		if err := json.Unmarshal(request.Body, &payload); err != nil {
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
		if tool["type"] != "function" {
			continue
		}

		definition := tool
		if nested, ok := tool["function"].(map[string]any); ok {
			definition = nested
		}
		parametersValue, exists := definition["parameters"]
		if !exists {
			continue
		}

		parameters, ok := parametersValue.(map[string]any)
		if !ok {
			if parametersValue != nil {
				continue
			}
			parameters = map[string]any{}
			definition["parameters"] = parameters
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
		llm.APIFormatOpenAIImageEdit:
		return true
	default:
		return false
	}
}

// applyUserAgentPassThrough creates a middleware that applies the User-Agent pass-through setting.
func applyUserAgentPassThrough(outbound *PersistentOutboundTransformer, systemService *biz.SystemService) pipeline.Middleware {
	return pipeline.OnRawRequest("user-agent-pass-through", func(ctx context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		currentChannel := outbound.GetCurrentChannel()
		if currentChannel == nil {
			return request, nil
		}

		var passThroughEnabled bool
		if currentChannel.Settings != nil && currentChannel.Settings.PassThroughUserAgent != nil {
			passThroughEnabled = *currentChannel.Settings.PassThroughUserAgent
		} else {
			globalPassThrough, err := systemService.UserAgentPassThrough(ctx)
			if err != nil {
				log.Warn(ctx, "failed to get global user agent pass through setting", log.Cause(err))

				passThroughEnabled = false
			} else {
				passThroughEnabled = globalPassThrough
			}
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
		} else if !codexChannel || request.Headers.Get("User-Agent") == "" {
			// Pass-through disabled: use AxonHub's default User-Agent
			request.Headers.Set("User-Agent", "axonhub/1.0")
		}

		return request, nil
	})
}

// captureRawProviderResponse stores the raw provider response on state for response pass-through.
func captureRawProviderResponse(outbound *PersistentOutboundTransformer, systemService *biz.SystemService) pipeline.Middleware {
	return pipeline.OnRawResponse("capture-raw-provider-response", func(ctx context.Context, response *httpclient.Response) (*httpclient.Response, error) {
		if outbound.isResponsePassThroughEnabled(ctx, systemService) {
			outbound.state.RawProviderResponse = response
		}

		return response, nil
	})
}

// applyPassThroughResponse replaces the transformed response with the raw provider response
// when PassThroughBody is enabled and the inbound/outbound API formats match.
func applyPassThroughResponse(outbound *PersistentOutboundTransformer, systemService *biz.SystemService) pipeline.Middleware {
	return pipeline.OnInboundRawResponse("pass-through-response", func(ctx context.Context, response *httpclient.Response) (*httpclient.Response, error) {
		if !outbound.isResponsePassThroughEnabled(ctx, systemService) {
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

		return rawResp, nil
	})
}

// captureRawProviderStream fans out raw provider stream events to both the pipeline
// (for transforms and LLM middlewares like connection tracking, performance recording)
// and a pass-through channel. The pipeline receives events via pipelineCh, while
// raw events are stored on state.RawStreamCh for pass-through delivery.
func captureRawProviderStream(outbound *PersistentOutboundTransformer, systemService *biz.SystemService) pipeline.Middleware {
	return captureRawProviderStreamWithTerminalGrace(outbound, systemService, delayedCodexResponsesTerminalGracePeriod)
}

func captureRawProviderStreamWithTerminalGrace(
	outbound *PersistentOutboundTransformer,
	systemService *biz.SystemService,
	terminalGracePeriod time.Duration,
) pipeline.Middleware {
	return pipeline.OnRawStream("capture-raw-provider-stream", func(ctx context.Context, stream streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*httpclient.StreamEvent], error) {
		if !outbound.isResponsePassThroughEnabled(ctx, systemService) {
			if !shouldRepairDelayedCodexResponsesTerminal(outbound) {
				return stream, nil
			}

			channel := outbound.GetCurrentChannel()
			return maybeRepairDelayedCodexResponsesTerminal(
				ctx,
				outbound,
				stream,
				terminalGracePeriod,
				newDelayedCodexResponsesUsageRecorder(ctx, outbound.state, channel.Name),
			), nil
		}

		channel := outbound.GetCurrentChannel()

		pipelineCh := make(chan *httpclient.StreamEvent, 64)
		rawStreamCh := make(chan *httpclient.StreamEvent, 64)
		outbound.state.RawStreamCh = rawStreamCh

		// Per-attempt local error storage: each attempt writes to its own variable so
		// concurrent defers from an abandoned goroutine and the new attempt's goroutine
		// never touch the same memory location, eliminating the data race on retries.
		var rawStreamErr error

		outbound.state.RawStreamErrRef = &rawStreamErr

		// Per-attempt cancelable context: PrepareForRetry / NextChannel call this cancel
		// to unblock the goroutine's channel sends and release the upstream HTTP connection
		// before the next attempt starts, preventing goroutine leaks.
		attemptCtx, cancel := context.WithCancel(ctx)
		stream = maybeRepairDelayedCodexResponsesTerminal(
			attemptCtx,
			outbound,
			stream,
			terminalGracePeriod,
			newDelayedCodexResponsesUsageRecorder(ctx, outbound.state, channel.Name),
		)

		var closeStreamOnce sync.Once
		closeStream := func() {
			closeStreamOnce.Do(func() {
				cancel()
				_ = stream.Close()
			})
		}
		outbound.state.RawStreamCancel = closeStream

		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Warn(ctx, "captureRawProviderStream goroutine panicked, recovering",
						log.Any("panic", r),
						log.String("channel", channel.Name),
					)
					rawStreamErr = fmt.Errorf("passthrough stream panic: %v", r)
				} else {
					rawStreamErr = stream.Err()
				}

				close(pipelineCh)
				close(rawStreamCh)
			}()
			// Ensure the context is cleaned up when the goroutine exits, regardless of
			// whether it finished naturally or was canceled by a retry.
			defer closeStream()

			for {
				select {
				case <-attemptCtx.Done():
					log.Debug(ctx, "context canceled before reading pass-through stream",
						log.String("channel", channel.Name))

					return
				default:
				}

				if !stream.Next() {
					return
				}

				event := stream.Current()
				// Use blocking sends so events are not silently dropped when a
				// consumer is slower than the upstream provider. Bail out on
				// attempt cancellation (retry) or request cancellation to avoid
				// blocking forever.
				select {
				case pipelineCh <- event:
				case <-attemptCtx.Done():
					log.Debug(ctx, "context canceled while sending pipeline event",
						log.String("channel", channel.Name))

					return
				}

				select {
				case rawStreamCh <- event:
				case <-attemptCtx.Done():
					log.Debug(ctx, "context canceled while sending pass-through event",
						log.String("channel", channel.Name))

					return
				}

				if IsTerminalStreamEvent(event) {
					return
				}
			}
		}()

		return &passThroughChannelStream{ctx: ctx, ch: pipelineCh, errRef: &rawStreamErr, cancel: closeStream}, nil
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
	if request.APIFormat != string(llm.APIFormatOpenAIResponse) {
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

// applyPassThroughStream returns a stream of raw provider events when PassThroughBody is enabled.
// A goroutine drains the transformed pipeline stream so that LLM middlewares (connection tracking,
// performance recording, rate limit tracking) still process events.
func applyPassThroughStream(outbound *PersistentOutboundTransformer, systemService *biz.SystemService) pipeline.Middleware {
	return pipeline.OnInboundRawStream("pass-through-response-stream", func(ctx context.Context, stream streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*httpclient.StreamEvent], error) {
		if !outbound.isResponsePassThroughEnabled(ctx, systemService) {
			return stream, nil
		}

		rawCh := outbound.state.RawStreamCh
		if rawCh == nil {
			return stream, nil
		}

		// Snapshot the current attempt's error reference. If a future retry replaces
		// state.RawStreamErrRef, this stream still reads from the correct variable.
		errRef := outbound.state.RawStreamErrRef
		cancel := outbound.state.RawStreamCancel

		channel := outbound.GetCurrentChannel()

		log.Debug(ctx, "applying pass-through stream",
			log.String("channel", channel.Name),
		)

		go func() {
			for stream.Next() {
				_ = stream.Current()
			}

			stream.Close()
		}()

		return &passThroughChannelStream{ctx: ctx, ch: rawCh, errRef: errRef, cancel: cancel}, nil
	})
}

// passThroughChannelStream wraps a channel as a Stream.
//
//nolint:containedctx // Required so Next() can observe request cancellation.
type passThroughChannelStream struct {
	ctx     context.Context
	ch      <-chan *httpclient.StreamEvent
	current *httpclient.StreamEvent
	errRef  *error
	cancel  context.CancelFunc
	once    sync.Once
	ctxDone bool
}

func (s *passThroughChannelStream) Next() bool {
	if s.ctx == nil {
		ev, ok := <-s.ch
		if !ok {
			return false
		}

		s.current = ev

		return true
	}

	if s.ctxDone || s.ctx.Err() != nil {
		s.ctxDone = true

		return s.nextBuffered()
	}

	select {
	case ev, ok := <-s.ch:
		if !ok {
			return false
		}

		s.current = ev

		return true
	case <-s.ctx.Done():
		// Client disconnect often races with the terminal event still sitting in
		// the channel (especially the pipeline drain path under pass-through).
		// Prefer draining already-buffered events over aborting, matching the
		// inbound/outbound Close() rule: cancel after a complete stream is still
		// completed.
		s.ctxDone = true

		return s.nextBuffered()
	}
}

// nextBuffered consumes buffered events after cancellation has been observed.
// It never blocks on the producer: the stream ends (and cancels upstream via
// Close) at the first moment the buffer is empty or the channel is closed.
func (s *passThroughChannelStream) nextBuffered() bool {
	select {
	case ev, ok := <-s.ch:
		if !ok {
			_ = s.Close()

			return false
		}

		s.current = ev

		return true
	default:
		_ = s.Close()

		return false
	}
}

func (s *passThroughChannelStream) Current() *httpclient.StreamEvent { return s.current }

func (s *passThroughChannelStream) Err() error {
	if s.errRef != nil {
		return *s.errRef
	}

	return nil
}

func (s *passThroughChannelStream) Close() error {
	s.once.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
	})

	return nil
}
