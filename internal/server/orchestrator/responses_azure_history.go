package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	entchannel "github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func usesAzureHistoryCompatibility(channel *biz.Channel) bool {
	return channel != nil && channel.Type == entchannel.TypeCodex && !channel.Credentials.IsOAuth() &&
		channel.Policies.AzureHistoryCompatibility
}

func applyAzureResponsesHistoryCompatibility(outbound *PersistentOutboundTransformer) pipeline.Middleware {
	return pipeline.OnRawRequest("azure-responses-history-compatibility", func(ctx context.Context, request *httpclient.Request) (*httpclient.Request, error) {
		if request == nil || !usesAzureHistoryCompatibility(outbound.GetCurrentChannel()) ||
			!responses.IsResponsesFormat(llm.APIFormat(request.APIFormat)) {
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
