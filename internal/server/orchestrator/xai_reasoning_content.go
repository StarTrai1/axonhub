package orchestrator

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

// xAI rejects null content beside an opaque reasoning checkpoint. Remove only
// that protocol field, after pass-through; keep the seal, IDs and cache prefix.
func applyXAIReasoningContent() pipeline.Middleware {
	return pipeline.OnRawRequest("xai-reasoning-content", func(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		if request == nil || !responses.IsResponsesFormat(llm.APIFormat(request.APIFormat)) {
			return request, nil
		}
		destination, err := url.Parse(request.URL)
		if err != nil {
			return nil, fmt.Errorf("parse reasoning replay destination: %w", err)
		}
		if !strings.EqualFold(destination.Hostname(), "api.x.ai") {
			return request, nil
		}
		body := request.Body
		for index, item := range gjson.GetBytes(body, "input").Array() {
			if item.Get("type").String() != "reasoning" || item.Get("content").Raw != "null" {
				continue
			}
			body, err = sjson.DeleteBytes(body, fmt.Sprintf("input.%d.content", index))
			if err != nil {
				return nil, fmt.Errorf("normalize xAI reasoning content: %w", err)
			}
		}
		request.Body = body
		return request, nil
	})
}
