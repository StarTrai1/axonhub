package responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
)

type searchCapabilityCache struct {
	unsupportedUntil atomic.Int64
}

func (c *searchCapabilityCache) unsupported(now time.Time) bool {
	if c == nil {
		return false
	}

	return now.UnixNano() < c.unsupportedUntil.Load()
}

func (c *searchCapabilityCache) markUnsupported(now time.Time, ttl time.Duration) {
	if c == nil {
		return
	}

	c.unsupportedUntil.Store(now.Add(ttl).UnixNano())
}

func (c *searchCapabilityCache) markSupported() {
	if c == nil {
		return
	}

	c.unsupportedUntil.Store(0)
}

type searchFallbackExecutor struct {
	inner      pipeline.Executor
	config     *SearchConfig
	capability *searchCapabilityCache
}

func (e *searchFallbackExecutor) Do(
	ctx context.Context,
	request *httpclient.Request,
) (*httpclient.Response, error) {
	if e == nil || e.inner == nil {
		return nil, errors.New("search fallback executor is not configured")
	}
	if request == nil {
		return nil, errors.New("search request is nil")
	}

	now := time.Now()
	if e.capability.unsupported(now) {
		return e.doResponsesFallback(ctx, request)
	}

	response, err := e.inner.Do(ctx, request)
	if err == nil {
		e.capability.markSupported()
		return response, nil
	}
	if !isStandaloneSearchEndpointUnsupported(err) {
		return nil, err
	}

	e.capability.markUnsupported(now, e.config.CapabilityNegativeTTL)

	return e.doResponsesFallback(ctx, request)
}

func (e *searchFallbackExecutor) DoStream(
	ctx context.Context,
	request *httpclient.Request,
) (streams.Stream[*httpclient.StreamEvent], error) {
	return e.inner.DoStream(ctx, request)
}

func (e *searchFallbackExecutor) doResponsesFallback(
	ctx context.Context,
	searchRequest *httpclient.Request,
) (*httpclient.Response, error) {
	fallbackRequest, streaming, err := buildResponsesSearchFallbackRequest(searchRequest, e.config)
	if err != nil {
		return nil, err
	}

	response, err := e.executeResponsesFallback(ctx, fallbackRequest, streaming)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, errors.New("responses search fallback returned no response")
	}

	body, err := normalizeResponsesSearchFallback(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize responses search fallback: %w", err)
	}

	headers := http.Header{"Content-Type": []string{"application/json"}}
	if response.Headers != nil {
		headers = response.Headers.Clone()
		headers.Set("Content-Type", "application/json")
	}

	return &httpclient.Response{
		StatusCode:  http.StatusOK,
		Headers:     headers,
		Body:        body,
		Request:     searchRequest,
		RawResponse: response.RawResponse,
		RawRequest:  response.RawRequest,
	}, nil
}

func (e *searchFallbackExecutor) executeResponsesFallback(
	ctx context.Context,
	request *httpclient.Request,
	streaming bool,
) (*httpclient.Response, error) {
	if !streaming {
		return e.inner.Do(ctx, request)
	}

	eventStream, err := e.inner.DoStream(ctx, request)
	if err != nil {
		return nil, err
	}
	if eventStream == nil {
		return nil, errors.New("responses search fallback returned no stream")
	}
	defer eventStream.Close()

	chunks, err := streams.All(eventStream)
	if err != nil {
		return nil, fmt.Errorf("failed to consume responses search fallback stream: %w", err)
	}

	body, _, err := AggregateStreamChunks(ctx, chunks)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate responses search fallback stream: %w", err)
	}

	return &httpclient.Response{
		StatusCode: http.StatusOK,
		Headers: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:    body,
		Request: request,
	}, nil
}

func isStandaloneSearchEndpointUnsupported(err error) bool {
	var httpErr *httpclient.Error
	if !errors.As(err, &httpErr) {
		return false
	}

	switch httpErr.StatusCode {
	case http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return strings.Contains(strings.ToLower(httpErr.URL), "/alpha/search")
	case http.StatusNotFound:
		if !strings.Contains(strings.ToLower(httpErr.URL), "/alpha/search") {
			return false
		}
	default:
		return false
	}

	message := strings.ToLower(strings.TrimSpace(string(httpErr.Body)))
	if message == "" || message == "not found" || message == "404 not found" {
		return true
	}

	markers := []string{
		"invalid url",
		"path not found",
		"route not found",
		"unknown endpoint",
		"unsupported endpoint",
		"cannot post",
		"not implemented",
	}
	for _, marker := range markers {
		if strings.Contains(message, marker) {
			return true
		}
	}

	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(httpErr.Body, &payload) == nil {
		message = strings.ToLower(strings.TrimSpace(payload.Error.Message))
		return message == "not found" || message == "path not found"
	}

	return false
}

type standaloneSearchFallbackRequest struct {
	ID              string          `json:"id"`
	Model           string          `json:"model"`
	Input           json.RawMessage `json:"input"`
	Commands        json.RawMessage `json:"commands"`
	Settings        json.RawMessage `json:"settings"`
	MaxOutputTokens *int64          `json:"max_output_tokens"`
}

type standaloneSearchFallbackSettings struct {
	UserLocation      json.RawMessage `json:"user_location"`
	SearchContextSize string          `json:"search_context_size"`
	ExternalWebAccess json.RawMessage `json:"external_web_access"`
	Filters           struct {
		AllowedDomains []string `json:"allowed_domains"`
		BlockedDomains []string `json:"blocked_domains"`
	} `json:"filters"`
}

type responsesSearchFallbackRequest struct {
	Model           string                        `json:"model"`
	Input           string                        `json:"input"`
	Tools           []responsesSearchFallbackTool `json:"tools"`
	ToolChoice      string                        `json:"tool_choice"`
	Stream          bool                          `json:"stream"`
	Store           bool                          `json:"store"`
	Include         []string                      `json:"include,omitempty"`
	MaxOutputTokens *int64                        `json:"max_output_tokens,omitempty"`
}

type responsesSearchFallbackTool struct {
	Type              string            `json:"type"`
	SearchContextSize string            `json:"search_context_size,omitempty"`
	ExternalWebAccess json.RawMessage   `json:"external_web_access,omitempty"`
	UserLocation      json.RawMessage   `json:"user_location,omitempty"`
	Filters           *WebSearchFilters `json:"filters,omitempty"`
}

func buildResponsesSearchFallbackRequest(
	searchRequest *httpclient.Request,
	config *SearchConfig,
) (*httpclient.Request, bool, error) {
	if config == nil {
		return nil, false, errors.New("search fallback config is nil")
	}

	var request standaloneSearchFallbackRequest
	if err := json.Unmarshal(searchRequest.Body, &request); err != nil {
		return nil, false, fmt.Errorf("failed to decode standalone search request: %w", err)
	}

	var settings standaloneSearchFallbackSettings
	if len(request.Settings) > 0 && string(request.Settings) != "null" {
		if err := json.Unmarshal(request.Settings, &settings); err != nil {
			return nil, false, fmt.Errorf("failed to decode standalone search settings: %w", err)
		}
	}

	fallbackModel, streaming := resolveSearchFallbackModel(request.Model, config.FallbackModel)

	tool := responsesSearchFallbackTool{
		Type:              "web_search",
		SearchContextSize: settings.SearchContextSize,
		ExternalWebAccess: normalizeSearchFallbackExternalWebAccess(settings.ExternalWebAccess),
		UserLocation:      append(json.RawMessage(nil), settings.UserLocation...),
	}
	if len(settings.Filters.AllowedDomains) > 0 {
		tool.Filters = &WebSearchFilters{
			AllowedDomains: append([]string(nil), settings.Filters.AllowedDomains...),
		}
	}

	promptPayload := struct {
		Input           json.RawMessage `json:"input,omitempty"`
		Commands        json.RawMessage `json:"commands,omitempty"`
		Settings        json.RawMessage `json:"settings,omitempty"`
		BlockedDomains []string        `json:"blocked_domains,omitempty"`
	}{
		Input:           request.Input,
		Commands:        request.Commands,
		Settings:        request.Settings,
		BlockedDomains: settings.Filters.BlockedDomains,
	}
	promptJSON, err := json.Marshal(promptPayload)
	if err != nil {
		return nil, false, fmt.Errorf("failed to encode standalone search fallback prompt: %w", err)
	}

	body, err := json.Marshal(responsesSearchFallbackRequest{
		Model:           fallbackModel,
		Input:           "Execute the following standalone web search request. Use web search, follow the command and settings exactly, and return a concise result with source citations. Request JSON: " + string(promptJSON),
		Tools:           []responsesSearchFallbackTool{tool},
		ToolChoice:      "auto",
		Stream:          streaming,
		Store:           false,
		Include:         []string{"web_search_call.action.sources"},
		MaxOutputTokens: request.MaxOutputTokens,
	})
	if err != nil {
		return nil, false, fmt.Errorf("failed to encode responses search fallback request: %w", err)
	}

	path := config.ResponsesEndpointPath
	if path == "" {
		path = "/responses"
	}

	headers := searchRequest.Headers.Clone()
	if headers == nil {
		headers = make(http.Header)
	}
	headers.Set("Content-Type", "application/json")
	if streaming {
		headers.Set("Accept", "text/event-stream")
		headers.Set("Cache-Control", "no-cache")
		headers.Set("Connection", "keep-alive")
	} else {
		headers.Set("Accept", "application/json")
		headers.Del("Cache-Control")
		headers.Del("Connection")
	}

	return &httpclient.Request{
		Method:                http.MethodPost,
		URL:                   config.ResponsesBaseURL + path,
		Headers:               headers,
		Body:                  body,
		Auth:                  searchRequest.Auth,
		RequestType:           string(llm.RequestTypeChat),
		APIFormat:             string(llm.APIFormatOpenAIResponse),
		SkipInboundQueryMerge: true,
	}, streaming, nil
}

func resolveSearchFallbackModel(requestModel, configuredFallback string) (string, bool) {
	model := strings.TrimSpace(requestModel)
	switch strings.ToLower(model) {
	case "gpt-5.6", "gpt-5.6-sol", "gpt-5.6-luna", "gpt-5.6-terra":
		return model, true
	default:
		return configuredFallback, false
	}
}

func normalizeSearchFallbackExternalWebAccess(value json.RawMessage) json.RawMessage {
	if len(value) == 0 || string(value) == "null" {
		return nil
	}

	var enabled bool
	if json.Unmarshal(value, &enabled) == nil {
		if enabled {
			return json.RawMessage("true")
		}

		return json.RawMessage("false")
	}

	var mode string
	if json.Unmarshal(value, &mode) != nil {
		return nil
	}

	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "live":
		return json.RawMessage("true")
	case "cached", "indexed":
		return json.RawMessage("false")
	default:
		return nil
	}
}

type standaloneSearchFallbackResponse struct {
	Output  string                           `json:"output"`
	Results []standaloneSearchFallbackResult `json:"results,omitempty"`
}

type standaloneSearchFallbackResult struct {
	Type  string `json:"type"`
	RefID string `json:"ref_id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

func normalizeResponsesSearchFallback(body []byte) ([]byte, error) {
	var response Response
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to decode responses fallback: %w", err)
	}
	if response.Status != nil && *response.Status != "completed" {
		return nil, fmt.Errorf("responses fallback did not complete: %s", *response.Status)
	}

	output := ""
	sources := make([]WebSearchSource, 0)
	for _, item := range response.Output {
		if item.Type == "web_search_call" && item.Action != nil && item.Action.WebSearch != nil {
			sources = append(sources, item.Action.WebSearch.Sources...)
		}
		if item.Type != "message" || item.Content == nil {
			continue
		}

		messageText := make([]string, 0)
		for _, content := range item.Content.Items {
			if content.Type != "output_text" || content.Text == nil {
				continue
			}
			if text := strings.TrimSpace(*content.Text); text != "" {
				messageText = append(messageText, text)
			}
			for _, annotation := range content.Annotations {
				if annotation.URLCitation == nil || annotation.URLCitation.URL == "" {
					continue
				}
				sources = append(sources, WebSearchSource{
					Type:  "url_citation",
					URL:   annotation.URLCitation.URL,
					Title: annotation.URLCitation.Title,
				})
			}
		}
		if len(messageText) > 0 {
			output = strings.Join(messageText, "\n\n")
		}
	}

	if output == "" {
		return nil, errors.New("responses fallback returned no output text")
	}

	seen := make(map[string]struct{}, len(sources))
	results := make([]standaloneSearchFallbackResult, 0, len(sources))
	for _, source := range sources {
		url := strings.TrimSpace(source.URL)
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}

		title := strings.TrimSpace(source.Title)
		if title == "" {
			title = url
		}
		results = append(results, standaloneSearchFallbackResult{
			Type:  "text_result",
			RefID: fmt.Sprintf("turn0search%d", len(results)),
			Title: title,
			URL:   url,
		})
	}

	return json.Marshal(standaloneSearchFallbackResponse{
		Output:  output,
		Results: results,
	})
}
