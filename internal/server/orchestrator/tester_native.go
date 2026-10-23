package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/samber/lo"
	"github.com/tidwall/gjson"

	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/pkg/xjson"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/decisions"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
	"github.com/looplj/axonhub/llm/transformer/typesafe"
)

func channelTestAPIFormat(channel *biz.Channel, model string) llm.APIFormat {
	if channel != nil {
		if channel.Type == entchannel.TypeCodex {
			return llm.APIFormatOpenAIResponse
		}
		endpoints := channel.ResolveEndpoints()
		entry, ok := channel.GetModelEntries()[model]
		if !ok {
			entry = channel.GetDirectModelEntries()[model]
		}
		forced := forcedAPIFormatsForCandidate(channel, []biz.ChannelModelEntry{entry}, model)
		if filtered := FilterEndpointsByAPIFormats(endpoints, forced); len(filtered) > 0 {
			return llm.APIFormat(filtered[0].APIFormat)
		} else if channel.Type != entchannel.TypeTypesafe && channel.Type != entchannel.TypeCloudflare {
			// Default OpenAI endpoints also advertise Decisions. A native connectivity
			// probe requires explicit endpoint/model selection, not mere availability.
			endpoints = channel.Endpoints
		}
		hasChat := false
		for _, endpoint := range endpoints {
			if endpoint.APIFormat == llm.APIFormatOpenAIChatCompletion.String() {
				hasChat = true
				break
			}
		}
		if hasChat {
			for _, endpoint := range endpoints {
				if endpoint.APIFormat == llm.APIFormatOpenAIDecisions.String() {
					return llm.APIFormatOpenAIDecisions
				}
			}
			return llm.APIFormatOpenAIChatCompletion
		}
		for _, endpoint := range endpoints {
			if endpoint.APIFormat == llm.APIFormatOpenAIDecisions.String() {
				return llm.APIFormatOpenAIDecisions
			}
			if endpoint.APIFormat == llm.APIFormatTypeSafeSystemOne.String() {
				return llm.APIFormatTypeSafeSystemOne
			}
		}
	}
	return llm.APIFormatOpenAIChatCompletion
}

func (processor *TestChannelOrchestrator) buildNativeChannelTestInput(ctx context.Context, model string, useStream bool, systemPrompt, userPrompt string, responsesWebSocket bool, apiFormat llm.APIFormat) (transformer.Inbound, *httpclient.Request, error) {
	if apiFormat == llm.APIFormatOpenAIResponse {
		request, err := buildResponsesChannelTestRequest(model, systemPrompt, userPrompt)
		return responses.NewInboundTransformer(), request, err
	}
	if apiFormat == llm.APIFormatOpenAIEmbedding {
		if useStream {
			return nil, nil, fmt.Errorf("embeddings do not support streaming")
		}
		prompts := &llm.Request{Model: model, Messages: []llm.Message{
			{Role: "system", Content: llm.MessageContent{Content: lo.ToPtr(systemPrompt)}},
			{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr(userPrompt)}},
		}}
		protected, err := processor.promptProtectionRuleService.Protect(ctx, prompts)
		if err != nil { return nil, nil, err }
		body, err := json.Marshal(openai.EmbeddingRequest{Model: model, Input: llm.EmbeddingInput{String: lo.FromPtr(protected.Messages[0].Content.Content) + "\n\n" + lo.FromPtr(protected.Messages[1].Content.Content)}})
		if err != nil { return nil, nil, err }
		return openai.NewEmbeddingInboundTransformer(), &httpclient.Request{Method: http.MethodPost, Path: "/v1/embeddings", Headers: http.Header{"Content-Type": {"application/json"}}, Body: body}, nil
	}
	if apiFormat != llm.APIFormatTypeSafeSystemOne && apiFormat != llm.APIFormatOpenAIDecisions {
		request, err := buildChannelTestRequest(model, useStream, systemPrompt, userPrompt, responsesWebSocket)
		return openai.NewInboundTransformer(), request, err
	}
	if useStream {
		return nil, nil, fmt.Errorf("%s does not support streaming", apiFormat)
	}
	// Apply the existing role-scoped rules before building a native request.
	prompts := &llm.Request{Model: model, Messages: []llm.Message{
		{Role: "system", Content: llm.MessageContent{Content: lo.ToPtr(systemPrompt)}},
		{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr(userPrompt)}},
	}}
	if apiFormat == llm.APIFormatOpenAIDecisions {
		// Native Decisions input is protected by the normal pipeline. Only the
		// system test instructions need protection before becoming a question.
		prompts.Messages = prompts.Messages[:1]
	}
	protected, err := processor.promptProtectionRuleService.Protect(ctx, prompts)
	if err != nil {
		return nil, nil, err
	}
	systemPrompt = lo.FromPtr(protected.Messages[0].Content.Content)
	if apiFormat == llm.APIFormatTypeSafeSystemOne {
		userPrompt = lo.FromPtr(protected.Messages[1].Content.Content)
	}
	var inbound transformer.Inbound
	var payload any
	if apiFormat == llm.APIFormatTypeSafeSystemOne {
		inbound = typesafe.NewSystemOneInboundTransformer()
		payload = struct {
			llm.SystemOneRequest

			Model string `json:"model"`
		}{Model: model, SystemOneRequest: llm.SystemOneRequest{
			State:     systemPrompt + "\n\n" + userPrompt,
			Questions: map[string]llm.SystemOneQuestion{"connection": {Type: "noul", Instructions: "Does the state contain a test prompt?"}},
		}}
	} else {
		inbound = decisions.NewInboundTransformer()
		payload = map[string]any{"model": model, "input": userPrompt, "questions": []map[string]any{
			{"name": "connection", "type": "predicate", "instructions": systemPrompt + "\nIs this a channel connectivity test?"},
		}}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	return inbound, &httpclient.Request{Method: http.MethodPost, Headers: http.Header{"Content-Type": {"application/json"}}, Body: body, RequestType: string(map[llm.APIFormat]llm.RequestType{llm.APIFormatTypeSafeSystemOne: llm.RequestTypeSystemOne, llm.APIFormatOpenAIDecisions: llm.RequestTypeDecisions}[apiFormat]), APIFormat: string(apiFormat)}, nil
}

// Probe the selected channel's protocol, independently of model name lists.
// The normal Codex transport finalizer supplies the same identity and required
// wire fields as a real request; probes do not fabricate a private Lite mode.
func buildResponsesChannelTestRequest(model, systemPrompt, userPrompt string) (*httpclient.Request, error) {
	body, err := json.Marshal(map[string]any{
		"model":               model,
		"instructions":        systemPrompt,
		"input":               []map[string]any{{"type": "message", "role": "user", "content": []map[string]string{{"type": "input_text", "text": userPrompt}}}},
		"stream":              true,
		"store":               false,
		"tools":               []any{},
		"parallel_tool_calls": true,
	})
	if err != nil {
		return nil, err
	}
	return &httpclient.Request{Method: http.MethodPost, Headers: http.Header{"Content-Type": {"application/json"}}, Body: body, APIFormat: llm.APIFormatOpenAIResponse.String(), Metadata: map[string]string{channelTestRequestMetadataKey: "true"}}, nil
}

func channelTestResponseMessage(body []byte, apiFormat llm.APIFormat) (*string, error) {
	if apiFormat == llm.APIFormatOpenAIEmbedding {
		response, err := xjson.To[llm.EmbeddingResponse](body)
		if err != nil { return nil, err }
		if len(response.Data) == 0 { return nil, fmt.Errorf("No embedding in response") }
		if dimensions := len(response.Data[0].Embedding.Embedding); dimensions > 0 { return lo.ToPtr(fmt.Sprintf("Embedding OK: %d dimensions", dimensions)), nil }
		if response.Data[0].Embedding.Base64 != "" { return lo.ToPtr("Embedding OK: base64 encoded vector"), nil }
		return nil, fmt.Errorf("Empty embedding vector in response")
	}
	if apiFormat == llm.APIFormatOpenAIResponse {
		if gjson.GetBytes(body, "status").String() != "completed" {
			return nil, fmt.Errorf("Responses probe did not complete successfully")
		}
		var text strings.Builder
		for _, item := range gjson.GetBytes(body, "output").Array() {
			for _, part := range item.Get("content").Array() {
				if part.Get("type").String() == "output_text" {
					text.WriteString(part.Get("text").String())
				}
			}
		}
		if text.Len() == 0 {
			return nil, fmt.Errorf("no text in Responses probe")
		}
		return lo.ToPtr(text.String()), nil
	}
	if apiFormat == llm.APIFormatOpenAIDecisions {
		var wire struct {
			Answers []json.RawMessage `json:"answers"`
		}
		if err := json.Unmarshal(body, &wire); err != nil {
			return nil, err
		}
		if len(wire.Answers) == 0 {
			return nil, fmt.Errorf("no answers in Decisions response")
		}
		return lo.ToPtr(string(body)), nil
	}
	if apiFormat == llm.APIFormatTypeSafeSystemOne {
		response, err := xjson.To[llm.SystemOneResponse](body)
		if err != nil {
			return nil, err
		}
		if len(response.Answers) == 0 {
			return nil, fmt.Errorf("no answers in System One response")
		}
		if _, ok := response.Answers["connection"]; !ok {
			return nil, fmt.Errorf("no connection answer in System One response")
		}
		answers, err := json.Marshal(response.Answers)
		if err != nil {
			return nil, err
		}
		return lo.ToPtr(string(answers)), nil
	}
	response, err := xjson.To[llm.Response](body)
	if err != nil {
		return nil, err
	}
	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("No message in response")
	}
	return response.Choices[0].Message.Content.Content, nil
}
