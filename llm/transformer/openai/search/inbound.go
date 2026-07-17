package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer"
)

var _ transformer.Inbound = (*InboundTransformer)(nil)

type rawResponse struct {
	statusCode int
	headers    http.Header
	body       []byte
}

const rawResponseMetadataKey = "openai_search_raw_response"

// InboundTransformer preserves the private Search API payload while exposing
// the model to AxonHub's normal routing and model-mapping pipeline.
type InboundTransformer struct{}

func NewInboundTransformer() *InboundTransformer {
	return &InboundTransformer{}
}

func (t *InboundTransformer) TransformRequest(_ context.Context, httpReq *httpclient.Request) (*llm.Request, error) {
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

	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(httpReq.Body, &req); err != nil {
		return nil, fmt.Errorf("%w: failed to decode search request: %w", transformer.ErrInvalidRequest, err)
	}

	if req.Model == "" {
		return nil, fmt.Errorf("%w: model is required", transformer.ErrInvalidRequest)
	}

	return &llm.Request{
		Model:       req.Model,
		RequestType: llm.RequestTypeChat,
		APIFormat:   llm.APIFormatOpenAISearch,
	}, nil
}

func (t *InboundTransformer) TransformResponse(_ context.Context, response *llm.Response) (*httpclient.Response, error) {
	if response == nil {
		return nil, fmt.Errorf("search response is nil")
	}

	raw, ok := response.TransformerMetadata[rawResponseMetadataKey].(*rawResponse)
	if !ok || raw == nil {
		return nil, fmt.Errorf("search response payload is missing")
	}

	return &httpclient.Response{
		StatusCode: raw.statusCode,
		Headers:    raw.headers.Clone(),
		Body:       append([]byte(nil), raw.body...),
	}, nil
}

func (t *InboundTransformer) TransformStream(
	_ context.Context,
	_ streams.Stream[*llm.Response],
) (streams.Stream[*httpclient.StreamEvent], error) {
	return nil, fmt.Errorf("%w: search streaming is not supported", transformer.ErrInvalidRequest)
}

func (t *InboundTransformer) TransformError(_ context.Context, rawErr error) *httpclient.Error {
	if rawErr == nil {
		return searchHTTPError(http.StatusInternalServerError, "internal server error", "internal_error", "")
	}

	if httpErr, ok := errors.AsType[*httpclient.Error](rawErr); ok {
		return httpErr
	}

	if llmErr, ok := errors.AsType[*llm.ResponseError](rawErr); ok {
		return searchHTTPError(llmErr.StatusCode, llmErr.Detail.Message, llmErr.Detail.Type, llmErr.Detail.Code)
	}

	if errors.Is(rawErr, transformer.ErrInvalidModel) {
		return searchHTTPError(http.StatusUnprocessableEntity, rawErr.Error(), "invalid_model_error", "")
	}

	if errors.Is(rawErr, transformer.ErrInvalidRequest) {
		return searchHTTPError(http.StatusBadRequest, rawErr.Error(), "invalid_request_error", "")
	}

	return searchHTTPError(http.StatusInternalServerError, rawErr.Error(), "internal_error", "")
}

func (t *InboundTransformer) AggregateStreamChunks(
	_ context.Context,
	_ []*httpclient.StreamEvent,
) ([]byte, llm.ResponseMeta, error) {
	return nil, llm.ResponseMeta{}, fmt.Errorf("%w: search streaming is not supported", transformer.ErrInvalidRequest)
}

func searchHTTPError(statusCode int, message, errorType, code string) *httpclient.Error {
	if statusCode == 0 {
		statusCode = http.StatusInternalServerError
	}

	body, _ := json.Marshal(struct {
		Error llm.ErrorDetail `json:"error"`
	}{
		Error: llm.ErrorDetail{
			Message: message,
			Type:    errorType,
			Code:    code,
		},
	})

	return &httpclient.Error{
		StatusCode: statusCode,
		Status:     http.StatusText(statusCode),
		Body:       body,
	}
}
