package orchestrator

import (
	"context"
	"fmt"
	"net/http"

	"golang.org/x/net/http/httpguts"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

// A WebSocket handshake may describe prewarm or a previous turn. Promote only
// the current frame's identity fields, before explicit channel header overrides.
// HTTP requests retain their existing header precedence. No input is rewritten.
func applyResponsesFrameMetadata() pipeline.Middleware {
	return pipeline.OnRawRequest("responses-frame-metadata", func(ctx context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		if !shared.IsResponsesWebSocket(ctx) || !responses.IsResponsesFormat(llm.APIFormat(request.APIFormat)) {
			return request, nil
		}
		identity := shared.ReadCodexRequestMetadata(nil, request.Body)
		values := map[string]string{
			codexSessionIDHeader: identity.SessionID, codexThreadIDHeader: identity.ThreadID,
			codexWindowIDHeader: identity.WindowID, codexTurnMetadataHeader: identity.RawTurnMetadata,
		}
		for _, value := range values {
			if !httpguts.ValidHeaderFieldValue(value) {
				return nil, fmt.Errorf("invalid Responses frame identity header")
			}
		}
		result := *request
		result.Headers = request.Headers.Clone()
		if result.Headers == nil {
			result.Headers = make(http.Header)
		}
		for header, value := range values {
			if value != "" {
				result.Headers.Set(header, value)
			}
		}
		return &result, nil
	})
}
