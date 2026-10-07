package decisions

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
	"github.com/looplj/axonhub/llm/transformer/openai"
)

type InboundTransformer struct {
	*openai.InboundTransformer
}

func NewInboundTransformer() *InboundTransformer {
	return &InboundTransformer{InboundTransformer: openai.NewInboundTransformer()}
}

func (t *InboundTransformer) TransformRequest(ctx context.Context, request *httpclient.Request) (*llm.Request, error) {
	if request == nil {
		return nil, fmt.Errorf("%w: request is nil", transformer.ErrInvalidRequest)
	}
	model, err := validateRequest(request.Body)
	if err != nil {
		return nil, err
	}
	return &llm.Request{
		Model:       model,
		RequestType: llm.RequestTypeDecisions,
		APIFormat:   llm.APIFormatOpenAIDecisions,
		Decisions:   append(json.RawMessage(nil), request.Body...),
		RawRequest:  request,
	}, nil
}

func (t *InboundTransformer) TransformResponse(ctx context.Context, response *llm.Response) (*httpclient.Response, error) {
	if response == nil || len(response.Decisions) == 0 {
		return nil, fmt.Errorf("decisions response is missing")
	}
	// usage.cost is gateway-owned; an unknown local price must not expose a
	// provider-supplied chat cost as the Decisions cost.
	body, err := sjson.DeleteBytes(append([]byte(nil), response.Decisions...), "usage.cost")
	if err != nil {
		return nil, err
	}
	if response.Model != "" {
		body, err = sjson.SetBytes(body, "model", response.Model)
		if err != nil {
			return nil, err
		}
	}
	if response.Usage != nil && response.Usage.Cost != nil {
		body, err = sjson.SetBytes(body, "usage.cost", *response.Usage.Cost)
		if err != nil {
			return nil, err
		}
	}
	return &httpclient.Response{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": {"application/json"}},
		Body:       body,
	}, nil
}

func (t *InboundTransformer) TransformStream(context.Context, streams.Stream[*llm.Response]) (streams.Stream[*httpclient.StreamEvent], error) {
	return nil, fmt.Errorf("%w: Decisions does not support streaming", transformer.ErrInvalidRequest)
}

func (t *InboundTransformer) AggregateStreamChunks(context.Context, []*httpclient.StreamEvent) ([]byte, llm.ResponseMeta, error) {
	return nil, llm.ResponseMeta{}, fmt.Errorf("Decisions does not support streaming")
}

type Config struct {
	BaseURL        string
	EndpointPath   string
	APIKeyProvider auth.APIKeyProvider
}

type OutboundTransformer struct {
	transformer.Outbound

	config Config
}

func NewOutboundTransformer(config Config) (*OutboundTransformer, error) {
	if config.APIKeyProvider == nil {
		return nil, fmt.Errorf("API key provider is required")
	}
	if config.BaseURL == "" {
		config.BaseURL = "https://api.openai.com/v1"
	}
	// The synchronous endpoint shares the API origin of Responses channels,
	// including channels whose primary transport is WebSocket.
	config.BaseURL = strings.TrimSpace(config.BaseURL)
	config.BaseURL = strings.Replace(config.BaseURL, "wss://", "https://", 1)
	config.BaseURL = strings.Replace(config.BaseURL, "ws://", "http://", 1)
	if config.EndpointPath == "" {
		config.BaseURL = transformer.NormalizeBaseURL(config.BaseURL, "v1")
		config.EndpointPath = "/decisions"
	} else {
		config.BaseURL = transformer.NormalizeBaseURL(config.BaseURL, "")
	}
	delegate, err := openai.NewOutboundTransformerWithConfig(&openai.Config{
		BaseURL: config.BaseURL, APIKeyProvider: config.APIKeyProvider,
	})
	if err != nil {
		return nil, err
	}
	return &OutboundTransformer{Outbound: delegate, config: config}, nil
}

func (t *OutboundTransformer) APIFormat() llm.APIFormat { return llm.APIFormatOpenAIDecisions }

func (t *OutboundTransformer) AllowPassThroughBody(_ context.Context, request *llm.Request, _ *httpclient.Request) bool {
	return request != nil && request.RequestType == llm.RequestTypeDecisions
}

func (t *OutboundTransformer) TransformRequest(ctx context.Context, request *llm.Request) (*httpclient.Request, error) {
	if request == nil || request.RequestType != llm.RequestTypeDecisions || len(request.Decisions) == 0 || request.Model == "" {
		return nil, fmt.Errorf("%w: native Decisions request is required", transformer.ErrInvalidRequest)
	}
	body, err := sjson.SetBytes(append([]byte(nil), request.Decisions...), "model", request.Model)
	if err != nil {
		return nil, err
	}
	return &httpclient.Request{
		Method:      http.MethodPost,
		URL:         t.config.BaseURL + t.config.EndpointPath,
		Headers:     http.Header{"Content-Type": {"application/json"}, "Accept": {"application/json"}},
		Body:        body,
		Auth:        &httpclient.AuthConfig{Type: "bearer", APIKey: t.config.APIKeyProvider.Get(ctx)},
		RequestType: string(llm.RequestTypeDecisions),
		APIFormat:   string(llm.APIFormatOpenAIDecisions),
	}, nil
}

func (t *OutboundTransformer) TransformResponse(ctx context.Context, response *httpclient.Response) (*llm.Response, error) {
	if response == nil {
		return nil, fmt.Errorf("decisions response is nil")
	}
	if response.StatusCode >= 400 {
		return nil, t.TransformError(ctx, &httpclient.Error{
			StatusCode: response.StatusCode, Headers: response.Headers, Body: response.Body,
		})
	}
	var wire struct {
		Model   string            `json:"model"`
		Answers []json.RawMessage `json:"answers"`
		Usage   *struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
			TotalTokens  int64 `json:"total_tokens"`
			InputDetails *struct {
				CachedTokens      int64 `json:"cached_tokens"`
				WriteCachedTokens int64 `json:"cache_write_tokens"`
			} `json:"input_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(response.Body, &wire); err != nil {
		return nil, fmt.Errorf("invalid decisions response: %w", err)
	}
	if len(wire.Answers) == 0 {
		return nil, fmt.Errorf("decisions response has no answers")
	}
	for _, raw := range wire.Answers {
		var answer struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &answer); err != nil || answer.Type == "" {
			return nil, fmt.Errorf("decisions response contains an invalid answer")
		}
	}
	result := &llm.Response{
		Model: wire.Model, RequestType: llm.RequestTypeDecisions, APIFormat: llm.APIFormatOpenAIDecisions,
		Decisions: append(json.RawMessage(nil), response.Body...),
	}
	if wire.Usage != nil {
		if wire.Usage.InputTokens < 0 || wire.Usage.OutputTokens < 0 || wire.Usage.TotalTokens < 0 {
			return nil, fmt.Errorf("decisions usage contains negative token counts")
		}
		result.Usage = &llm.Usage{
			PromptTokens: wire.Usage.InputTokens, CompletionTokens: wire.Usage.OutputTokens, TotalTokens: wire.Usage.TotalTokens,
		}
		if wire.Usage.InputDetails != nil {
			if wire.Usage.InputDetails.CachedTokens < 0 || wire.Usage.InputDetails.WriteCachedTokens < 0 {
				return nil, fmt.Errorf("decisions usage contains negative cache token counts")
			}
			result.Usage.PromptTokensDetails = &llm.PromptTokensDetails{
				CachedTokens: wire.Usage.InputDetails.CachedTokens, WriteCachedTokens: wire.Usage.InputDetails.WriteCachedTokens,
			}
		}
	}
	return result, nil
}

func (t *OutboundTransformer) TransformStream(context.Context, *httpclient.Request, streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*llm.Response], error) {
	return nil, fmt.Errorf("Decisions does not support streaming")
}

func (t *OutboundTransformer) AggregateStreamChunks(context.Context, *httpclient.Request, []*httpclient.StreamEvent) ([]byte, llm.ResponseMeta, error) {
	return nil, llm.ResponseMeta{}, fmt.Errorf("Decisions does not support streaming")
}
