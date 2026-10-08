package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/samber/lo"

	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/pkg/xjson"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/decisions"
	"github.com/looplj/axonhub/llm/transformer/typesafe"
)

func channelTestAPIFormat(channel *biz.Channel, model string) llm.APIFormat {
	if channel != nil {
		endpoints := channel.ResolveEndpoints()
		entry, ok := channel.GetModelEntries()[model]
		if !ok {
			entry = channel.GetDirectModelEntries()[model]
		}
		forced := forcedAPIFormatsForCandidate(channel, []biz.ChannelModelEntry{entry}, model)
		if filtered := FilterEndpointsByAPIFormats(endpoints, forced); len(filtered) > 0 {
			endpoints = filtered[:1]
		} else if channel.Type != entchannel.TypeTypesafe {
			// Default OpenAI endpoints also advertise Decisions. A native connectivity
			// probe requires explicit endpoint/model selection, not mere availability.
			endpoints = channel.Endpoints
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

func channelTestResponseMessage(body []byte, apiFormat llm.APIFormat) (*string, error) {
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
