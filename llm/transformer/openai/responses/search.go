package responses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/auth"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/openai"
)

var (
	_ transformer.Inbound  = (*SearchInboundTransformer)(nil)
	_ transformer.Outbound = (*SearchOutboundTransformer)(nil)
)

type searchRequestEnvelope struct {
	ID    string `json:"id"`
	Model string `json:"model"`
}

// SearchInboundTransformer handles Codex standalone web-search requests.
type SearchInboundTransformer struct{}

func NewSearchInboundTransformer() *SearchInboundTransformer {
	return &SearchInboundTransformer{}
}

func (t *SearchInboundTransformer) TransformRequest(
	ctx context.Context,
	httpReq *httpclient.Request,
) (*llm.Request, error) {
	if httpReq == nil {
		return nil, fmt.Errorf("%w: http request is nil", transformer.ErrInvalidRequest)
	}
	if len(httpReq.Body) == 0 {
		return nil, fmt.Errorf("%w: request body is empty", transformer.ErrInvalidRequest)
	}

	contentType := httpReq.Headers.Get("Content-Type")
	if contentType != "" && !strings.Contains(strings.ToLower(contentType), "application/json") {
		return nil, fmt.Errorf("%w: unsupported content type: %s", transformer.ErrInvalidRequest, contentType)
	}

	var envelope searchRequestEnvelope
	if err := json.Unmarshal(httpReq.Body, &envelope); err != nil {
		return nil, fmt.Errorf("%w: failed to decode search request: %w", transformer.ErrInvalidRequest, err)
	}
	if envelope.Model == "" {
		return nil, fmt.Errorf("%w: model is required", transformer.ErrInvalidRequest)
	}

	return &llm.Request{
		Model:       envelope.Model,
		RequestType: llm.RequestTypeSearch,
		APIFormat:   llm.APIFormatOpenAISearch,
		RawRequest:  httpReq,
		Search: &llm.SearchRequest{
			Raw: append([]byte(nil), httpReq.Body...),
		},
	}, nil
}

func (t *SearchInboundTransformer) TransformResponse(
	ctx context.Context,
	llmResp *llm.Response,
) (*httpclient.Response, error) {
	if llmResp == nil || llmResp.Search == nil || len(llmResp.Search.Raw) == 0 {
		return nil, fmt.Errorf("search response is empty")
	}

	return &httpclient.Response{
		StatusCode: http.StatusOK,
		Headers: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: append([]byte(nil), llmResp.Search.Raw...),
	}, nil
}

func (t *SearchInboundTransformer) TransformStream(
	ctx context.Context,
	stream streams.Stream[*llm.Response],
) (streams.Stream[*httpclient.StreamEvent], error) {
	return nil, fmt.Errorf("standalone search does not support streaming")
}

func (t *SearchInboundTransformer) TransformError(ctx context.Context, err error) *httpclient.Error {
	return NewInboundTransformer().TransformError(ctx, err)
}

func (t *SearchInboundTransformer) AggregateStreamChunks(
	ctx context.Context,
	chunks []*httpclient.StreamEvent,
) ([]byte, llm.ResponseMeta, error) {
	return nil, llm.ResponseMeta{}, fmt.Errorf("standalone search does not support streaming")
}

// SearchConfig configures the standalone search outbound endpoint.
type SearchConfig struct {
	BaseURL        string
	EndpointPath   string
	APIKeyProvider auth.APIKeyProvider
}

// SearchOutboundTransformer forwards standalone search JSON without coupling it
// to the Responses WebSocket transport.
type SearchOutboundTransformer struct {
	config *SearchConfig
}

func NewSearchOutboundTransformer(baseURL, apiKey string) (*SearchOutboundTransformer, error) {
	return NewSearchOutboundTransformerWithConfig(&SearchConfig{
		BaseURL:        baseURL,
		APIKeyProvider: auth.NewStaticKeyProvider(apiKey),
	})
}

func NewSearchOutboundTransformerWithConfig(config *SearchConfig) (*SearchOutboundTransformer, error) {
	if config == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if config.APIKeyProvider == nil {
		return nil, fmt.Errorf("API key provider is required")
	}
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, fmt.Errorf("base URL is required")
	}

	normalized := *config
	normalized.BaseURL = normalizeSearchBaseURL(normalized.BaseURL, normalized.EndpointPath != "")

	return &SearchOutboundTransformer{config: &normalized}, nil
}

func normalizeSearchBaseURL(baseURL string, customEndpoint bool) string {
	baseURL = strings.TrimSpace(baseURL)
	if strings.HasPrefix(baseURL, "wss://") {
		baseURL = "https://" + strings.TrimPrefix(baseURL, "wss://")
	} else if strings.HasPrefix(baseURL, "ws://") {
		baseURL = "http://" + strings.TrimPrefix(baseURL, "ws://")
	}

	if customEndpoint {
		return transformer.NormalizeBaseURL(baseURL, "")
	}

	return transformer.NormalizeBaseURL(baseURL, "v1")
}

func (t *SearchOutboundTransformer) APIFormat() llm.APIFormat {
	return llm.APIFormatOpenAISearch
}

func (t *SearchOutboundTransformer) TransformRequest(
	ctx context.Context,
	llmReq *llm.Request,
) (*httpclient.Request, error) {
	if llmReq == nil || llmReq.Search == nil || len(llmReq.Search.Raw) == 0 {
		return nil, fmt.Errorf("%w: search request is empty", transformer.ErrInvalidRequest)
	}
	if llmReq.Model == "" {
		return nil, fmt.Errorf("%w: model is required", transformer.ErrInvalidRequest)
	}

	body, err := sjson.SetBytes(llmReq.Search.Raw, "model", llmReq.Model)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to set search model: %w", transformer.ErrInvalidRequest, err)
	}

	path := "/alpha/search"
	if t.config.EndpointPath != "" {
		path = t.config.EndpointPath
	}

	return &httpclient.Request{
		Method: http.MethodPost,
		URL:    t.config.BaseURL + path,
		Headers: http.Header{
			"Content-Type": []string{"application/json"},
			"Accept":       []string{"application/json"},
		},
		Body: body,
		Auth: &httpclient.AuthConfig{
			Type:   httpclient.AuthTypeBearer,
			APIKey: t.config.APIKeyProvider.Get(ctx),
		},
		RequestType:          string(llm.RequestTypeSearch),
		APIFormat:            string(llm.APIFormatOpenAISearch),
		SkipInboundQueryMerge: true,
	}, nil
}

func (t *SearchOutboundTransformer) TransformResponse(
	ctx context.Context,
	httpResp *httpclient.Response,
) (*llm.Response, error) {
	if httpResp == nil {
		return nil, fmt.Errorf("http response is nil")
	}
	if httpResp.StatusCode >= http.StatusBadRequest {
		return nil, t.TransformError(ctx, &httpclient.Error{
			StatusCode: httpResp.StatusCode,
			Body:       httpResp.Body,
		})
	}
	if len(httpResp.Body) == 0 || !json.Valid(httpResp.Body) {
		return nil, fmt.Errorf("standalone search returned invalid JSON")
	}

	var requestEnvelope searchRequestEnvelope
	if httpResp.Request != nil {
		_ = json.Unmarshal(httpResp.Request.Body, &requestEnvelope)
	}

	return &llm.Response{
		ID:          requestEnvelope.ID,
		Model:       requestEnvelope.Model,
		Object:      "search.response",
		RequestType: llm.RequestTypeSearch,
		APIFormat:   llm.APIFormatOpenAISearch,
		Search: &llm.SearchResponse{
			Raw: append([]byte(nil), httpResp.Body...),
		},
	}, nil
}

func (t *SearchOutboundTransformer) TransformStream(
	ctx context.Context,
	req *httpclient.Request,
	stream streams.Stream[*httpclient.StreamEvent],
) (streams.Stream[*llm.Response], error) {
	return nil, fmt.Errorf("standalone search does not support streaming")
}

func (t *SearchOutboundTransformer) TransformError(ctx context.Context, err *httpclient.Error) *llm.ResponseError {
	return openai.TransformOpenAIError(ctx, err)
}

func (t *SearchOutboundTransformer) AggregateStreamChunks(
	ctx context.Context,
	req *httpclient.Request,
	chunks []*httpclient.StreamEvent,
) ([]byte, llm.ResponseMeta, error) {
	return nil, llm.ResponseMeta{}, errors.New("standalone search does not support streaming")
}
