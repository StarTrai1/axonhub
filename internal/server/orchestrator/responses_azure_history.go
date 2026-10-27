package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

const azureHistoryReplayField = "azure_history_replay"

func usesAzureHistoryCompatibility(outbound *PersistentOutboundTransformer) bool {
	return outbound != nil && azureHistoryRecoveryRequested(outbound.state, outbound.GetCurrentChannel())
}

func azureHistoryRecoveryRequested(state *PersistenceState, channel *biz.Channel) bool {
	if state == nil || channel == nil || channel.Type != entchannel.TypeCodex || channel.Credentials.IsOAuth() {
		return false
	}
	for _, rule := range state.responsesRejectedStatusRules[channel.ID] {
		if rule.fieldName() == azureHistoryReplayField {
			return true
		}
	}
	return false
}

// An explicit resource rejection permits one recovery on this exact request
// and destination. It never becomes a channel-wide or learned cache policy.
func requestAzureHistoryRecovery(ctx context.Context, outbound *PersistentOutboundTransformer, failure error) bool {
	state := outbound.state
	channel := outbound.GetCurrentChannel()
	if ctx.Err() != nil || channel == nil || channel.Type != entchannel.TypeCodex || channel.Credentials.IsOAuth() ||
		state.RawProviderRequest == nil || !responses.IsResponsesFormat(llm.APIFormat(state.RawProviderRequest.APIFormat)) {
		return false
	}
	target, parseErr := url.Parse(state.RawProviderRequest.URL)
	if parseErr != nil || target.Hostname() == "" || strings.EqualFold(target.Hostname(), "chatgpt.com") || strings.EqualFold(target.Hostname(), "api.openai.com") {
		return false
	}
	code, message, param, badRequest := responsesBadRequestDetails(failure)
	if !badRequest || !responsesResourceMismatch(code, message) {
		return false
	}
	if param != "" && param != "input" {
		match := responsesResourceItemIDParamPattern.FindStringSubmatch(param)
		if len(match) != 2 {
			return false
		}
		item := gjson.GetBytes(state.RawProviderRequest.Body, "input."+match[1])
		if !item.IsObject() || item.Get("id").Type != gjson.String || item.Get("id").String() == "" {
			return false
		}
	}
	scope := responsesMetadataKey(channel.ID, state.RawProviderRequest)
	if rememberResponsesRejectedStatusRule(state, channel.ID, responsesRejectedStatusRule{
		index: -1, field: azureHistoryReplayField, resourceScope: &scope,
	}) {
		state.responsesRejectedStatusRetryChannel = channel.ID
	}
	return true
}

func applyAzureResponsesHistoryCompatibility(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return pipeline.OnRawRequest("azure-responses-history-compatibility", func(ctx context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		if request == nil || !usesAzureHistoryCompatibility(outbound) ||
			!responses.IsResponsesFormat(llm.APIFormat(request.APIFormat)) {
			return request, nil
		}
		scope := responsesMetadataKey(outbound.GetCurrentChannel().ID, request)
		matched := false
		for _, rule := range outbound.state.responsesRejectedStatusRules[scope.channelID] {
			matched = matched || rule.fieldName() == azureHistoryReplayField && rule.resourceScope != nil && *rule.resourceScope == scope
		}
		if !matched {
			return request, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		body, err := azureCompatibleResponsesHistory(request.Body)
		if err != nil {
			return nil, &llm.ResponseError{StatusCode: 400, Detail: llm.ErrorDetail{
				Type: "invalid_request_error", Code: "azure_history_unavailable", Param: "input",
				Message: "Azure history compatibility requires complete readable history. Restore the original conversation or start a new one: " + err.Error(),
			}}
		}
		cloned := *request
		cloned.Body = body
		if len(request.JSONBody) > 0 {
			cloned.JSONBody = body
		}
		cloned.Headers = request.Headers.Clone()
		cloned.Headers.Del(codexTurnStateHeader)
		cloned.ResponsesStateless = true
		return &cloned, nil
	})
}

// Adapt the Azure history-detachment approach from codex-api-compat to the
// gateway's retained-history contract. Only protocol-owned item IDs are removed;
// arbitrary tool payload fields and call_id associations remain byte-preserved.
// Opaque references/checkpoints must be materialized by preceding middleware,
// never silently dropped to make an incomplete conversation pass validation.
func azureCompatibleResponsesHistory(body []byte) ([]byte, error) {
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return nil, fmt.Errorf("invalid Responses request object")
	}
	if gjson.GetBytes(body, "previous_response_id").String() != "" ||
		gjson.GetBytes(body, "conversation").String() != "" {
		return nil, fmt.Errorf("unresolved conversation reference")
	}
	input := gjson.GetBytes(body, "input")
	if input.IsArray() && len(input.Array()) > 0 {
		if _, safe := responsesResourceHistorySupportsRecovery(body); !safe {
			return nil, fmt.Errorf("history contains unresolved items, encrypted tool data, or missing tool calls")
		}
		if _, complete := responsesExplicitHistorySupportsRecovery(body); !complete {
			return nil, fmt.Errorf("native checkpoint has no readable retained history")
		}
		var err error
		body, _, err = stripResponsesRejectedStatus(body, []responsesRejectedStatusRule{
			{index: -1, field: "id", detachReasoningID: true},
			{itemType: "reasoning", index: -1, field: "encrypted_content", dropItem: true},
		})
		if err != nil {
			return nil, err
		}
	} else if input.Exists() && !input.IsArray() && input.Type != gjson.String && input.Type != gjson.Null {
		return nil, fmt.Errorf("unsupported Responses input shape")
	}
	var err error
	body, err = sjson.DeleteBytes(body, "previous_response_id")
	if err != nil {
		return nil, err
	}
	include := gjson.GetBytes(body, "include")
	if include.IsArray() {
		entries := make([]json.RawMessage, 0, len(include.Array()))
		for _, entry := range include.Array() {
			if entry.Type != gjson.String || entry.String() != "reasoning.encrypted_content" {
				entries = append(entries, json.RawMessage(entry.Raw))
			}
		}
		if len(entries) != len(include.Array()) {
			if len(entries) == 0 {
				body, err = sjson.DeleteBytes(body, "include")
			} else {
				body, err = sjson.SetBytes(body, "include", entries)
			}
		}
	}
	return body, err
}
