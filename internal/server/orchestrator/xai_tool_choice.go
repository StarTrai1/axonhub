package orchestrator

import (
	"context"
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer"
)

// applyXAIHostedToolChoice adapts singleton hosted selections for the xAI CLI
// subscription proxy. Run after pass-through so native Responses are covered.
func applyXAIHostedToolChoice(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return pipeline.OnRawRequest("xai-hosted-tool-choice", func(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		selected := outbound.GetCurrentChannel()
		if request == nil || request.APIFormat != string(llm.APIFormatOpenAIResponse) ||
			selected == nil || selected.Type != channel.TypeXaiSubscription {
			return request, nil
		}
		choice := gjson.GetBytes(request.Body, "tool_choice")
		toolType := choice.Get("type").String()
		mode := "required"
		if toolType == "allowed_tools" {
			allowed := choice.Get("tools")
			if !allowed.IsArray() || len(allowed.Array()) != 1 {
				return request, nil
			}
			toolType = allowed.Array()[0].Get("type").String()
			mode = choice.Get("mode").String()
			if mode != "auto" && mode != "required" {
				return request, nil
			}
		}
		if toolType != "web_search" && toolType != "image_generation" {
			return request, nil
		}

		// "required" alone would allow an unrelated function or x_search call.
		// Keep the selected tool declarations, including all their options.
		kept := make([]byte, 0)
		kept = append(kept, '[')
		count := 0
		for _, tool := range gjson.GetBytes(request.Body, "tools").Array() {
			if tool.Get("type").String() != toolType {
				continue
			}
			if count > 0 {
				kept = append(kept, ',')
			}
			kept = append(kept, tool.Raw...)
			count++
		}
		if count == 0 {
			return nil, fmt.Errorf("%w: selected xAI hosted tool %q is not declared", transformer.ErrInvalidRequest, toolType)
		}
		kept = append(kept, ']')
		body, err := sjson.SetRawBytes(request.Body, "tools", kept)
		if err != nil {
			return nil, fmt.Errorf("normalize xAI hosted tools: %w", err)
		}
		body, err = sjson.SetBytes(body, "tool_choice", mode)
		if err != nil {
			return nil, fmt.Errorf("normalize xAI hosted tool choice: %w", err)
		}
		request.Body = body
		return request, nil
	})
}
