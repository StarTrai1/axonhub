package codex

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/auth"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/oauth"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

// SearchParams configures a standalone Codex search endpoint.
type SearchParams struct {
	TokenProvider oauth.TokenGetter
	BaseURL       string
	EndpointPath  string
}

// SearchOutboundTransformer applies Codex authentication to standalone search requests.
type SearchOutboundTransformer struct {
	tokens   oauth.TokenGetter
	delegate *responses.SearchOutboundTransformer
}

// NewSearchOutboundTransformer creates a standalone search transformer.
func NewSearchOutboundTransformer(params SearchParams) (*SearchOutboundTransformer, error) {
	if params.TokenProvider == nil {
		return nil, errors.New("token provider is required")
	}

	delegate, err := responses.NewSearchOutboundTransformerWithConfig(&responses.SearchConfig{
		BaseURL:        params.BaseURL,
		EndpointPath:   params.EndpointPath,
		APIKeyProvider: auth.NewStaticKeyProvider("dummy"),
	})
	if err != nil {
		return nil, err
	}

	return &SearchOutboundTransformer{
		tokens:   params.TokenProvider,
		delegate: delegate,
	}, nil
}

func (t *SearchOutboundTransformer) APIFormat() llm.APIFormat {
	return llm.APIFormatOpenAISearch
}

func (t *SearchOutboundTransformer) TransformRequest(
	ctx context.Context,
	llmReq *llm.Request,
) (*httpclient.Request, error) {
	hreq, err := t.delegate.TransformRequest(ctx, llmReq)
	if err != nil {
		return nil, err
	}

	creds, err := t.tokens.Get(ctx)
	if err != nil {
		return nil, err
	}
	if creds == nil || strings.TrimSpace(creds.AccessToken) == "" {
		return nil, errors.New("access token is empty")
	}

	hreq.Auth = &httpclient.AuthConfig{
		Type:   httpclient.AuthTypeBearer,
		APIKey: creds.AccessToken,
	}

	var rawHeaders http.Header
	if llmReq != nil && llmReq.RawRequest != nil {
		rawHeaders = llmReq.RawRequest.Headers
	}

	if originator := rawHeaders.Get("Originator"); originator != "" {
		hreq.Headers.Set("Originator", originator)
	} else {
		hreq.Headers.Set("Originator", AxonHubOriginator)
	}
	if userAgent := rawHeaders.Get("User-Agent"); userAgent != "" {
		hreq.Headers.Set("User-Agent", userAgent)
	}
	for _, header := range PassthroughHeaders {
		if value := rawHeaders.Get(header); value != "" {
			hreq.Headers.Set(header, value)
		}
	}
	if accountID := ExtractChatGPTAccountIDFromJWT(creds.AccessToken); accountID != "" {
		hreq.Headers.Set("Chatgpt-Account-Id", accountID)
	}

	return hreq, nil
}

func (t *SearchOutboundTransformer) TransformResponse(
	ctx context.Context,
	resp *httpclient.Response,
) (*llm.Response, error) {
	return t.delegate.TransformResponse(ctx, resp)
}

func (t *SearchOutboundTransformer) TransformStream(
	ctx context.Context,
	req *httpclient.Request,
	stream streams.Stream[*httpclient.StreamEvent],
) (streams.Stream[*llm.Response], error) {
	return t.delegate.TransformStream(ctx, req, stream)
}

func (t *SearchOutboundTransformer) TransformError(
	ctx context.Context,
	err *httpclient.Error,
) *llm.ResponseError {
	return t.delegate.TransformError(ctx, err)
}

func (t *SearchOutboundTransformer) AggregateStreamChunks(
	ctx context.Context,
	req *httpclient.Request,
	chunks []*httpclient.StreamEvent,
) ([]byte, llm.ResponseMeta, error) {
	return t.delegate.AggregateStreamChunks(ctx, req, chunks)
}
