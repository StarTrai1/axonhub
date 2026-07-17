package search

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/auth"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer"
)

var _ transformer.Outbound = (*OutboundTransformer)(nil)

const searchModelMetadataKey = "openai_search_model"

type Config struct {
	BaseURL        string
	RawURL         bool
	EndpointPath   string
	APIKeyProvider auth.APIKeyProvider
}

type OutboundTransformer struct {
	config Config
}

func NewOutboundTransformerWithConfig(config *Config) (*OutboundTransformer, error) {
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}

	if config.APIKeyProvider == nil {
		return nil, fmt.Errorf("API key provider is required")
	}

	normalized := *config
	normalized.BaseURL = httpBaseURL(normalized.BaseURL)
	if strings.HasSuffix(normalized.BaseURL, "##") {
		normalized.RawURL = true
		normalized.BaseURL = strings.TrimSuffix(normalized.BaseURL, "##")
	} else if normalized.EndpointPath != "" {
		normalized.BaseURL = transformer.NormalizeBaseURL(normalized.BaseURL, "")
	} else {
		normalized.BaseURL = transformer.NormalizeBaseURL(normalized.BaseURL, "v1")
	}

	return &OutboundTransformer{config: normalized}, nil
}

func httpBaseURL(baseURL string) string {
	switch {
	case strings.HasPrefix(baseURL, "wss://"):
		return "https://" + strings.TrimPrefix(baseURL, "wss://")
	case strings.HasPrefix(baseURL, "ws://"):
		return "http://" + strings.TrimPrefix(baseURL, "ws://")
	default:
		return baseURL
	}
}

func (t *OutboundTransformer) APIFormat() llm.APIFormat {
	return llm.APIFormatOpenAISearch
}

func (t *OutboundTransformer) TransformRequest(ctx context.Context, llmReq *llm.Request) (*httpclient.Request, error) {
	if llmReq == nil {
		return nil, fmt.Errorf("search request is nil")
	}

	if llmReq.RawRequest == nil || len(llmReq.RawRequest.Body) == 0 {
		return nil, fmt.Errorf("%w: raw search request is missing", transformer.ErrInvalidRequest)
	}

	body, err := sjson.SetBytes(append([]byte(nil), llmReq.RawRequest.Body...), "model", llmReq.Model)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to patch search model: %w", transformer.ErrInvalidRequest, err)
	}

	return &httpclient.Request{
		Method: http.MethodPost,
		URL:    t.requestURL(),
		Headers: http.Header{
			"Accept":       []string{"application/json"},
			"Content-Type": []string{"application/json"},
		},
		Body: body,
		Auth: &httpclient.AuthConfig{
			Type:   httpclient.AuthTypeBearer,
			APIKey: t.config.APIKeyProvider.Get(ctx),
		},
		RequestType: string(llm.RequestTypeChat),
		APIFormat:   llm.APIFormatOpenAISearch.String(),
		TransformerMetadata: map[string]any{
			searchModelMetadataKey: llmReq.Model,
		},
		SkipInboundQueryMerge: true,
	}, nil
}

func (t *OutboundTransformer) requestURL() string {
	if t.config.RawURL {
		return strings.TrimRight(t.config.BaseURL, "/")
	}

	if t.config.EndpointPath != "" {
		return t.config.BaseURL + t.config.EndpointPath
	}

	return t.config.BaseURL + "/alpha/search"
}

func (t *OutboundTransformer) TransformResponse(_ context.Context, httpResp *httpclient.Response) (*llm.Response, error) {
	if httpResp == nil {
		return nil, fmt.Errorf("search response is nil")
	}

	if httpResp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("HTTP error %d: %s", httpResp.StatusCode, strings.TrimSpace(string(httpResp.Body)))
	}

	if len(httpResp.Body) == 0 {
		return nil, fmt.Errorf("search response body is empty")
	}

	model := ""
	if httpResp.Request != nil && httpResp.Request.TransformerMetadata != nil {
		model, _ = httpResp.Request.TransformerMetadata[searchModelMetadataKey].(string)
	}

	content := "search response"
	return &llm.Response{
		Object:      "search.response",
		Model:       model,
		RequestType: llm.RequestTypeChat,
		APIFormat:   llm.APIFormatOpenAISearch,
		Choices: []llm.Choice{{
			Index: 0,
			Message: &llm.Message{
				Role: "assistant",
				Content: llm.MessageContent{
					Content: &content,
				},
			},
		}},
		TransformerMetadata: map[string]any{
			rawResponseMetadataKey: &rawResponse{
				statusCode: httpResp.StatusCode,
				headers:    httpResp.Headers.Clone(),
				body:       append([]byte(nil), httpResp.Body...),
			},
		},
	}, nil
}

func (t *OutboundTransformer) TransformStream(
	_ context.Context,
	_ *httpclient.Request,
	_ streams.Stream[*httpclient.StreamEvent],
) (streams.Stream[*llm.Response], error) {
	return nil, fmt.Errorf("%w: search streaming is not supported", transformer.ErrInvalidRequest)
}

func (t *OutboundTransformer) TransformError(_ context.Context, rawErr *httpclient.Error) *llm.ResponseError {
	if rawErr == nil {
		return &llm.ResponseError{
			StatusCode: http.StatusInternalServerError,
			Detail: llm.ErrorDetail{
				Message: http.StatusText(http.StatusInternalServerError),
				Type:    "api_error",
			},
		}
	}

	var upstream struct {
		Error llm.ErrorDetail `json:"error"`
	}
	if err := json.Unmarshal(rawErr.Body, &upstream); err == nil && upstream.Error.Message != "" {
		return &llm.ResponseError{StatusCode: rawErr.StatusCode, Detail: upstream.Error}
	}

	return &llm.ResponseError{
		StatusCode: rawErr.StatusCode,
		Detail: llm.ErrorDetail{
			Message: strings.TrimSpace(string(rawErr.Body)),
			Type:    "api_error",
		},
	}
}

func (t *OutboundTransformer) AggregateStreamChunks(
	_ context.Context,
	_ *httpclient.Request,
	_ []*httpclient.StreamEvent,
) ([]byte, llm.ResponseMeta, error) {
	return nil, llm.ResponseMeta{}, fmt.Errorf("%w: search streaming is not supported", transformer.ErrInvalidRequest)
}
