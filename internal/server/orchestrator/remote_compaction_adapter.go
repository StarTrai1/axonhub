package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	gocache "github.com/patrickmn/go-cache"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

const (
	remoteCompactionHistoryLookupLimit = 1024
	remoteCompactionCacheHeader        = "X-Axonhub-Remote-Compaction-Cache"
	remoteCompactionCacheExpiration    = 24 * time.Hour
	remoteCompactionCacheCleanup       = time.Hour

	localCompactionPrompt = `You are performing a CONTEXT CHECKPOINT COMPACTION. Create a handoff summary for another LLM that will resume the task.

Include:
- Current progress and key decisions made
- Important context, constraints, or user preferences
- What remains to be done (clear next steps)
- Any critical data, examples, or references needed to continue

Be concise, structured, and focused on helping the next LLM seamlessly continue the work.`

	localCompactionSummaryPrefix = "Another language model started to solve this problem and produced a summary of its thinking process. You also have access to the state of the tools that were used by that language model. Use this to build on the work that has already been done and avoid duplicating work. Here is the summary produced by the other language model, use the information in this summary to assist with your own analysis:"
)

type remoteCompactionAdapter struct {
	requestService   *biz.RequestService
	usageLogService  *biz.UsageLogService
	systemService    *biz.SystemService
	summaries        *gocache.Cache
	summaryGenerator singleflight.Group
}

type remoteCompactionReference struct {
	ID               string
	EncryptedContent string
	Index            int
}

type remoteCompactionSource struct {
	body    []byte
	headers http.Header
}

func newRemoteCompactionAdapter(
	requestService *biz.RequestService,
	usageLogService *biz.UsageLogService,
	systemService *biz.SystemService,
) *remoteCompactionAdapter {
	return &remoteCompactionAdapter{
		requestService:  requestService,
		usageLogService: usageLogService,
		systemService:   systemService,
		summaries:       gocache.New(remoteCompactionCacheExpiration, remoteCompactionCacheCleanup),
	}
}

func adaptRemoteCompactionForUnsupportedChannels(
	inbound *PersistentInboundTransformer,
	adapter *remoteCompactionAdapter,
	executor pipeline.Executor,
) pipeline.Middleware {
	return pipeline.OnLlmRequest("adapt-remote-compaction", func(ctx context.Context, req *llm.Request) (*llm.Request, error) {
		if adapter == nil || inbound == nil || req == nil || executor == nil {
			return req, nil
		}
		if req.APIFormat != llm.APIFormatOpenAIResponse || req.RequestType == llm.RequestTypeCompact {
			return req, nil
		}
		if hasRemoteCompactionCapableCandidate(inbound.state.ChannelModelsCandidates) {
			return req, nil
		}
		if !hasUnsupportedCodexCandidate(inbound.state.ChannelModelsCandidates) {
			return req, nil
		}
		if req.RawRequest == nil || len(rawRequestPayload(req.RawRequest)) == 0 {
			return req, nil
		}

		ref, threadID, rawModel, err := parseRemoteCompactionRequest(rawRequestPayload(req.RawRequest))
		if err != nil {
			return nil, err
		}
		if ref == nil {
			return req, nil
		}
		if ref.ID == "" || threadID == "" {
			return nil, errors.New("remote compaction local fallback requires a compaction id and Codex thread id")
		}

		cacheKey := remoteCompactionCacheKey(ref)
		summary, err := adapter.summaryForCompaction(
			ctx,
			cacheKey,
			ref,
			threadID,
			rawModel,
			inbound.state,
			executor,
		)
		if err != nil {
			return nil, fmt.Errorf("adapt remote compaction for unsupported Codex channel: %w", err)
		}

		adaptedBody, err := replaceRemoteCompactionWithLocalSummary(rawRequestPayload(req.RawRequest), summary)
		if err != nil {
			return nil, err
		}
		adaptedRawRequest := cloneRawRequest(req.RawRequest)
		adaptedRawRequest.Body = adaptedBody
		if len(adaptedRawRequest.JSONBody) > 0 {
			adaptedRawRequest.JSONBody = adaptedBody
		}

		adapted, err := responses.NewInboundTransformer().TransformRequest(ctx, adaptedRawRequest)
		if err != nil {
			return nil, fmt.Errorf("parse locally compacted Responses request: %w", err)
		}
		adapted.Model = req.Model
		adapted.ReasoningEffort = req.ReasoningEffort
		adapted.RawRequest = adaptedRawRequest

		inbound.state.RawRequest = adaptedRawRequest
		inbound.state.LlmRequest = adapted

		log.Info(ctx, "adapted remote compaction history for unsupported Codex channel",
			log.String("compaction_id", ref.ID),
			log.String("thread_id", threadID),
			log.Int("candidate_count", len(inbound.state.ChannelModelsCandidates)),
		)

		return adapted, nil
	})
}

func hasRemoteCompactionCapableCandidate(candidates []*ChannelModelsCandidate) bool {
	for _, candidate := range candidates {
		if candidate != nil && candidate.Channel != nil &&
			candidate.Channel.Type == channel.TypeCodex &&
			candidate.Channel.Policies.SupportsRemoteCompaction {
			return true
		}
	}

	return false
}

func hasUnsupportedCodexCandidate(candidates []*ChannelModelsCandidate) bool {
	for _, candidate := range candidates {
		if candidate != nil && candidate.Channel != nil &&
			candidate.Channel.Type == channel.TypeCodex &&
			!candidate.Channel.Policies.SupportsRemoteCompaction {
			return true
		}
	}

	return false
}

func (a *remoteCompactionAdapter) summaryForCompaction(
	ctx context.Context,
	cacheKey string,
	ref *remoteCompactionReference,
	threadID string,
	model string,
	state *PersistenceState,
	executor pipeline.Executor,
) (string, error) {
	if cached, ok := a.summaries.Get(cacheKey); ok {
		if summary, valid := cached.(string); valid && summary != "" {
			return summary, nil
		}
	}

	value, err, _ := a.summaryGenerator.Do(cacheKey, func() (any, error) {
		if cached, ok := a.summaries.Get(cacheKey); ok {
			if summary, valid := cached.(string); valid && summary != "" {
				return summary, nil
			}
		}

		summary, source, err := a.findStoredSummaryOrSource(ctx, cacheKey, ref.ID, threadID, model, state)
		if err != nil {
			return "", err
		}
		if summary == "" {
			if source == nil {
				return "", fmt.Errorf("original request for compaction %q was not retained", ref.ID)
			}
			summary, err = a.generateLocalSummary(ctx, cacheKey, source, state, executor)
			if err != nil {
				return "", err
			}
		}

		a.summaries.SetDefault(cacheKey, summary)
		return summary, nil
	})
	if err != nil {
		return "", err
	}

	summary, ok := value.(string)
	if !ok || summary == "" {
		return "", errors.New("local compaction summary is empty")
	}

	return summary, nil
}

func (a *remoteCompactionAdapter) findStoredSummaryOrSource(
	ctx context.Context,
	cacheKey string,
	compactionID string,
	threadID string,
	model string,
	state *PersistenceState,
) (string, *remoteCompactionSource, error) {
	if a.requestService == nil || state == nil || state.APIKey == nil {
		return "", nil, errors.New("request history is unavailable")
	}

	recent, err := a.requestService.FindRecentCompletedRequests(
		ctx,
		state.APIKey.ID,
		state.APIKey.ProjectID,
		model,
		llm.APIFormatOpenAIResponse,
		remoteCompactionHistoryLookupLimit,
	)
	if err != nil {
		return "", nil, err
	}

	var source *remoteCompactionSource
	for _, prior := range recent {
		if storedRemoteCompactionCacheKey(prior.RequestHeaders) == cacheKey {
			responseBody, loadErr := a.requestService.LoadResponseBody(ctx, prior)
			if loadErr != nil {
				return "", nil, loadErr
			}
			if summary := extractAssistantOutputText(responseBody); summary != "" {
				return summary, nil, nil
			}
		}

		if source != nil {
			continue
		}

		body, loadErr := a.requestService.LoadRequestBody(ctx, prior)
		if loadErr != nil {
			return "", nil, loadErr
		}
		if !isMatchingCompactionSource(body, threadID) {
			continue
		}

		executions, queryErr := prior.QueryExecutions().All(ctx)
		if queryErr != nil {
			return "", nil, fmt.Errorf("query compaction source executions: %w", queryErr)
		}
		for _, execution := range executions {
			responseBody, loadErr := a.requestService.LoadRequestExecutionResponseBody(ctx, execution)
			if loadErr != nil {
				return "", nil, loadErr
			}
			if responseContainsCompactionID(responseBody, compactionID) {
				source = &remoteCompactionSource{
					body:    append([]byte(nil), body...),
					headers: decodeStoredHeaders(prior.RequestHeaders),
				}
				break
			}
		}
	}

	return "", source, nil
}

func (a *remoteCompactionAdapter) generateLocalSummary(
	ctx context.Context,
	cacheKey string,
	source *remoteCompactionSource,
	state *PersistenceState,
	executor pipeline.Executor,
) (string, error) {
	body, err := buildLocalCompactionRequest(source.body)
	if err != nil {
		return "", err
	}

	headers := source.headers.Clone()
	headers.Del("Authorization")
	headers.Del("Content-Length")
	headers.Set(remoteCompactionCacheHeader, cacheKey)
	updateCompactionImplementationHeader(headers)

	logRequest := &httpclient.Request{
		Method:      http.MethodPost,
		Headers:     headers.Clone(),
		ContentType: "application/json",
		Body:        body,
		RequestType: string(llm.RequestTypeChat),
		APIFormat:   string(llm.APIFormatOpenAIResponse),
	}
	bridgeRequest, err := responses.NewInboundTransformer().TransformRequest(ctx, logRequest)
	if err != nil {
		return "", fmt.Errorf("parse local compaction bridge request: %w", err)
	}
	bridgeRequest.RawRequest = logRequest

	var bridgeRecord *ent.Request
	if a.requestService != nil {
		bridgeRecord, err = a.requestService.CreateRequest(ctx, bridgeRequest, logRequest, llm.APIFormatOpenAIResponse)
		if err != nil {
			return "", fmt.Errorf("persist local compaction bridge request: %w", err)
		}
	}

	var attemptErrors []error
	for _, candidate := range state.ChannelModelsCandidates {
		if candidate == nil || candidate.Channel == nil || candidate.Channel.Type != channel.TypeCodex || len(candidate.Models) == 0 {
			continue
		}

		summary, attemptErr := a.generateLocalSummaryWithCandidate(
			ctx,
			body,
			headers,
			bridgeRecord,
			candidate,
			state,
			executor,
		)
		if attemptErr == nil {
			return summary, nil
		}
		attemptErrors = append(attemptErrors, attemptErr)
	}

	err = errors.Join(attemptErrors...)
	if err == nil {
		err = errors.New("no unsupported Codex candidate was available for local compaction")
	}
	if bridgeRecord != nil {
		_ = a.requestService.UpdateRequestStatusFromError(context.WithoutCancel(ctx), bridgeRecord.ID, err)
	}

	return "", err
}

func (a *remoteCompactionAdapter) generateLocalSummaryWithCandidate(
	ctx context.Context,
	body []byte,
	headers http.Header,
	bridgeRecord *ent.Request,
	candidate *ChannelModelsCandidate,
	parentState *PersistenceState,
	executor pipeline.Executor,
) (string, error) {
	rawRequest := &httpclient.Request{
		Method:      http.MethodPost,
		Headers:     headers.Clone(),
		ContentType: "application/json",
		Body:        append([]byte(nil), body...),
		RequestType: string(llm.RequestTypeChat),
		APIFormat:   string(llm.APIFormatOpenAIResponse),
	}
	rawRequest.Headers.Del(remoteCompactionCacheHeader)

	bridgeRequest, err := responses.NewInboundTransformer().TransformRequest(ctx, rawRequest)
	if err != nil {
		return "", err
	}
	bridgeRequest.RawRequest = rawRequest

	attemptState := &PersistenceState{
		RequestService:          a.requestService,
		UsageLogService:         a.usageLogService,
		ChannelService:          parentState.ChannelService,
		Proxy:                   parentState.Proxy,
		OriginalModel:           bridgeRequest.Model,
		RawRequest:              rawRequest,
		LlmRequest:              bridgeRequest,
		OriginalRequestStream:   bridgeRequest.Stream,
		Request:                 bridgeRecord,
		ChannelModelsCandidates: []*ChannelModelsCandidate{candidate},
	}
	_, outbound := NewPersistentTransformers(attemptState, responses.NewInboundTransformer())
	providerRequest, err := outbound.TransformRequest(ctx, bridgeRequest)
	if err != nil {
		return "", err
	}
	providerRequest = httpclient.MergeInboundRequest(providerRequest, bridgeRequest.RawRequest)
	providerRequest, err = httpclient.FinalizeAuthHeaders(providerRequest)
	if err != nil {
		return "", fmt.Errorf("finalize local compaction bridge authentication: %w", err)
	}

	requestMiddlewares := []pipeline.Middleware{
		applyPassThroughRequestBody(outbound, a.systemService),
		applyOverrideRequestBody(outbound),
		applyUserAgentPassThrough(outbound, a.systemService),
		applyOverrideRequestHeaders(outbound),
	}
	for _, middleware := range requestMiddlewares {
		providerRequest, err = middleware.OnOutboundRawRequest(ctx, providerRequest)
		if err != nil {
			return "", err
		}
	}
	providerRequest.Headers.Del(remoteCompactionCacheHeader)

	var executionRecord *ent.RequestExecution
	if bridgeRecord != nil && a.requestService != nil {
		entry := candidate.Models[0]
		executionRecord, err = a.requestService.CreateRequestExecution(
			ctx,
			candidate.Channel,
			entry.ActualModel,
			bridgeRecord,
			*providerRequest,
			llm.APIFormatOpenAIResponse,
			attemptState.PassThroughApplied,
		)
		if err != nil {
			return "", err
		}
		attemptState.RequestExec = executionRecord
	}

	customizedExecutor := outbound.CustomizeExecutor(executor)
	perf := &biz.PerformanceRecord{
		ChannelID: candidate.Channel.ID,
		StartTime: time.Now(),
		Stream:    true,
	}
	attemptState.Perf = perf
	stream, err := customizedExecutor.DoStream(ctx, providerRequest)
	if err != nil {
		a.markBridgeExecutionFailed(ctx, executionRecord, err)
		return "", err
	}
	stream = maybeRepairDelayedCodexResponsesTerminal(
		ctx,
		outbound,
		stream,
		delayedCodexResponsesTerminalGracePeriod,
		newDelayedCodexResponsesUsageRecorder(ctx, attemptState, candidate.Channel.Name),
	)
	defer func() {
		_ = stream.Close()
	}()

	chunks, err := collectLocalCompactionBridgeStream(stream, perf)
	if err != nil {
		a.markBridgeExecutionFailed(ctx, executionRecord, err)
		return "", err
	}

	responseBody, meta, err := outbound.AggregateStreamChunks(ctx, providerRequest, chunks)
	if err != nil {
		a.markBridgeExecutionFailed(ctx, executionRecord, err)
		return "", err
	}
	summary := extractAssistantOutputText(responseBody)
	if summary == "" {
		err = errors.New("local compaction provider returned no assistant summary")
		a.markBridgeExecutionFailed(ctx, executionRecord, err)
		return "", err
	}
	if meta.Usage != nil {
		if tokenCount := meta.Usage.GetCompletionTokens(); tokenCount != nil {
			perf.CompletionTokens = *tokenCount
		}
	}
	if !perf.RequestCompleted {
		perf.MarkSuccess()
	}
	if attemptState.ChannelService != nil {
		attemptState.ChannelService.AsyncRecordPerformance(ctx, perf)
	}
	firstTokenLatencyMs, requestLatencyMs, _ := perf.Calculate()
	metrics := &biz.LatencyMetrics{LatencyMs: &requestLatencyMs}
	if perf.FirstTokenTime != nil {
		metrics.FirstTokenLatencyMs = &firstTokenLatencyMs
	}

	if bridgeRecord != nil && a.requestService != nil {
		persistCtx := context.WithoutCancel(ctx)
		if executionRecord != nil {
			if persistErr := a.requestService.UpdateRequestExecutionCompleted(persistCtx, executionRecord.ID, meta.ID, responseBody, metrics); persistErr != nil {
				log.Warn(persistCtx, "failed to persist local compaction bridge execution", log.Cause(persistErr))
			}
			if meta.Usage != nil && a.usageLogService != nil {
				if _, usageErr := a.usageLogService.CreateUsageLogFromRequest(persistCtx, bridgeRecord, executionRecord, meta.Usage); usageErr != nil {
					log.Warn(persistCtx, "failed to persist local compaction bridge usage", log.Cause(usageErr))
				}
			}
		}
		if persistErr := a.requestService.UpdateRequestChannelID(persistCtx, bridgeRecord.ID, candidate.Channel.ID); persistErr != nil {
			log.Warn(persistCtx, "failed to persist local compaction bridge channel", log.Cause(persistErr))
		}
		if persistErr := a.requestService.UpdateRequestCompleted(persistCtx, bridgeRecord.ID, meta.ID, responseBody, metrics); persistErr != nil {
			log.Warn(persistCtx, "failed to persist local compaction bridge response", log.Cause(persistErr))
		}
	}

	return summary, nil
}

func collectLocalCompactionBridgeStream(
	stream streams.Stream[*httpclient.StreamEvent],
	perf *biz.PerformanceRecord,
) ([]*httpclient.StreamEvent, error) {
	chunks := make([]*httpclient.StreamEvent, 0, 32)
	for stream.Next() {
		event := stream.Current()
		if event == nil {
			continue
		}
		if perf != nil && perf.FirstTokenTime == nil {
			perf.MarkFirstToken()
		}
		chunks = append(chunks, event)
		if isTerminalStreamEvent(event) {
			if perf != nil && !perf.RequestCompleted {
				perf.MarkSuccess()
			}
			break
		}
	}
	if err := stream.Err(); err != nil {
		return nil, err
	}

	return chunks, nil
}

func (a *remoteCompactionAdapter) markBridgeExecutionFailed(ctx context.Context, execution *ent.RequestExecution, err error) {
	if execution == nil || a.requestService == nil || err == nil {
		return
	}
	if updateErr := a.requestService.UpdateRequestExecutionStatusFromError(context.WithoutCancel(ctx), execution.ID, err); updateErr != nil {
		log.Warn(ctx, "failed to persist local compaction bridge error", log.Cause(updateErr))
	}
}

func parseRemoteCompactionRequest(body []byte) (*remoteCompactionReference, string, string, error) {
	envelope, input, err := decodeResponsesInput(body)
	if err != nil {
		return nil, "", "", err
	}

	var ref *remoteCompactionReference
	for index, raw := range input {
		var item struct {
			Type             string `json:"type"`
			ID               string `json:"id"`
			EncryptedContent string `json:"encrypted_content"`
		}
		if json.Unmarshal(raw, &item) != nil {
			continue
		}
		if item.Type == remoteCompactionItemType || item.Type == legacyRemoteCompactionSummaryType {
			ref = &remoteCompactionReference{
				ID:               item.ID,
				EncryptedContent: item.EncryptedContent,
				Index:            index,
			}
		}
	}

	return ref, responseEnvelopeThreadID(envelope), responseEnvelopeModel(envelope), nil
}

func buildLocalCompactionRequest(body []byte) ([]byte, error) {
	envelope, input, err := decodeResponsesInput(body)
	if err != nil {
		return nil, err
	}

	triggerIndex := -1
	for index, raw := range input {
		if rawInputItemType(raw) == remoteCompactionTriggerType {
			triggerIndex = index
		}
	}
	if triggerIndex < 0 {
		return nil, errors.New("stored compaction source has no compaction_trigger item")
	}

	input[triggerIndex] = localCompactionMessage(localCompactionPrompt)
	input = input[:triggerIndex+1]
	if err := setResponseEnvelopeInput(envelope, input); err != nil {
		return nil, err
	}
	setResponseEnvelopeBool(envelope, "stream", true)
	updateCompactionImplementationMetadata(envelope)

	return json.Marshal(envelope)
}

func replaceRemoteCompactionWithLocalSummary(body []byte, summary string) ([]byte, error) {
	if strings.TrimSpace(summary) == "" {
		return nil, errors.New("local compaction summary is empty")
	}
	envelope, input, err := decodeResponsesInput(body)
	if err != nil {
		return nil, err
	}

	lastCompaction := -1
	for index, raw := range input {
		switch rawInputItemType(raw) {
		case remoteCompactionItemType, legacyRemoteCompactionSummaryType:
			lastCompaction = index
		}
	}
	if lastCompaction < 0 {
		return nil, errors.New("Responses request has no remote compaction item")
	}

	adapted := make([]json.RawMessage, 0, len(input))
	for index, raw := range input {
		switch rawInputItemType(raw) {
		case remoteCompactionItemType, legacyRemoteCompactionSummaryType:
			if index == lastCompaction {
				adapted = append(adapted, localCompactionMessage(localCompactionSummaryPrefix+"\n"+summary))
			}
		default:
			adapted = append(adapted, raw)
		}
	}
	if err := setResponseEnvelopeInput(envelope, adapted); err != nil {
		return nil, err
	}

	return json.Marshal(envelope)
}

func decodeResponsesInput(body []byte) (map[string]json.RawMessage, []json.RawMessage, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, nil, fmt.Errorf("decode Responses request: %w", err)
	}
	var input []json.RawMessage
	if err := json.Unmarshal(envelope["input"], &input); err != nil {
		return nil, nil, fmt.Errorf("decode Responses input: %w", err)
	}

	return envelope, input, nil
}

func setResponseEnvelopeInput(envelope map[string]json.RawMessage, input []json.RawMessage) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	envelope["input"] = raw

	return nil
}

func setResponseEnvelopeBool(envelope map[string]json.RawMessage, key string, value bool) {
	if value {
		envelope[key] = json.RawMessage("true")
	} else {
		envelope[key] = json.RawMessage("false")
	}
}

func responseEnvelopeThreadID(envelope map[string]json.RawMessage) string {
	var metadata map[string]json.RawMessage
	if json.Unmarshal(envelope["client_metadata"], &metadata) != nil {
		return ""
	}
	var threadID string
	_ = json.Unmarshal(metadata["thread_id"], &threadID)

	return threadID
}

func responseEnvelopeModel(envelope map[string]json.RawMessage) string {
	var model string
	_ = json.Unmarshal(envelope["model"], &model)

	return model
}

func rawInputItemType(raw json.RawMessage) string {
	var item struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &item)

	return item.Type
}

func localCompactionMessage(text string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{
		"type": "message",
		"role": "user",
		"content": []map[string]string{{
			"type": "input_text",
			"text": text,
		}},
	})

	return raw
}

func updateCompactionImplementationMetadata(envelope map[string]json.RawMessage) {
	var metadata map[string]json.RawMessage
	if json.Unmarshal(envelope["client_metadata"], &metadata) != nil {
		return
	}
	var turnMetadata string
	if json.Unmarshal(metadata["x-codex-turn-metadata"], &turnMetadata) != nil || turnMetadata == "" {
		return
	}
	updated := updateCompactionImplementationJSON(turnMetadata)
	raw, err := json.Marshal(updated)
	if err != nil {
		return
	}
	metadata["x-codex-turn-metadata"] = raw
	raw, err = json.Marshal(metadata)
	if err == nil {
		envelope["client_metadata"] = raw
	}
}

func updateCompactionImplementationHeader(headers http.Header) {
	value := headers.Get("X-Codex-Turn-Metadata")
	if value != "" {
		headers.Set("X-Codex-Turn-Metadata", updateCompactionImplementationJSON(value))
	}
}

func updateCompactionImplementationJSON(value string) string {
	var metadata map[string]any
	if json.Unmarshal([]byte(value), &metadata) != nil {
		return value
	}
	compaction, ok := metadata["compaction"].(map[string]any)
	if !ok {
		return value
	}
	compaction["implementation"] = "responses"
	raw, err := json.Marshal(metadata)
	if err != nil {
		return value
	}

	return string(raw)
}

func isMatchingCompactionSource(body []byte, threadID string) bool {
	envelope, input, err := decodeResponsesInput(body)
	if err != nil || responseEnvelopeThreadID(envelope) != threadID {
		return false
	}
	for _, raw := range input {
		if rawInputItemType(raw) == remoteCompactionTriggerType {
			return true
		}
	}

	return false
}

func responseContainsCompactionID(body []byte, compactionID string) bool {
	if compactionID == "" || len(body) == 0 {
		return false
	}
	var envelope struct {
		Output []struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		} `json:"output"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return false
	}
	for _, item := range envelope.Output {
		if (item.Type == remoteCompactionItemType || item.Type == legacyRemoteCompactionSummaryType) && item.ID == compactionID {
			return true
		}
	}

	return false
}

func extractAssistantOutputText(body []byte) string {
	var envelope struct {
		Output []struct {
			Type    string `json:"type"`
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return ""
	}
	var pieces []string
	for _, item := range envelope.Output {
		if item.Type != "message" || item.Role != "assistant" {
			continue
		}
		for _, part := range item.Content {
			if part.Type == "output_text" && part.Text != "" {
				pieces = append(pieces, part.Text)
			}
		}
	}

	return strings.Join(pieces, "\n")
}

func remoteCompactionCacheKey(ref *remoteCompactionReference) string {
	hash := sha256.Sum256([]byte(ref.ID + "\x00" + ref.EncryptedContent))

	return hex.EncodeToString(hash[:])
}

func storedRemoteCompactionCacheKey(rawHeaders []byte) string {
	headers := decodeStoredHeaders(rawHeaders)

	return headers.Get(remoteCompactionCacheHeader)
}

func decodeStoredHeaders(raw []byte) http.Header {
	headers := make(http.Header)
	_ = json.Unmarshal(raw, &headers)

	return headers
}

func cloneRawRequest(source *httpclient.Request) *httpclient.Request {
	if source == nil {
		return nil
	}
	cloned := *source
	cloned.Headers = source.Headers.Clone()
	cloned.Body = append([]byte(nil), source.Body...)
	cloned.JSONBody = append([]byte(nil), source.JSONBody...)

	return &cloned
}

func rawRequestPayload(request *httpclient.Request) []byte {
	if request == nil {
		return nil
	}
	if len(request.Body) > 0 {
		return request.Body
	}

	return request.JSONBody
}
