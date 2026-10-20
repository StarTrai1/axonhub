package codex

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

// Normalize the wire dialect once at the transport boundary. Archives and the
// client's Lite input retain their original tool declarations. A relay receives
// public Responses tool declarations, not a Lite body without its Lite header.
func (t *OutboundTransformer) normalizeResponsesContract(request *httpclient.Request) *httpclient.Request {
	if t == nil || request == nil || !gjson.ValidBytes(request.Body) {
		return request
	}
	lite := t.isOfficialCodex() && strings.EqualFold(request.Headers.Get(responses.ResponsesLiteHeader), "true")
	if lite {
		return request
	}
	body := request.Body
	if !t.isOfficialCodex() {
		body = liftRelayToolDeclarations(body)
	}
	// The Codex backend requires an instructions string even for a user-only
	// health probe. Absence and the empty string carry the same prompt meaning.
	if value := gjson.GetBytes(body, "instructions"); !value.Exists() || value.Type == gjson.Null {
		body, _ = sjson.SetBytes(body, "instructions", "")
	}
	if !gjson.GetBytes(body, "tools").Exists() {
		body, _ = sjson.SetRawBytes(body, "tools", []byte("[]"))
	}
	if !gjson.GetBytes(body, "parallel_tool_calls").Exists() {
		body, _ = sjson.SetBytes(body, "parallel_tool_calls", true)
	}
	if len(gjson.GetBytes(body, "tools").Array()) == 0 && gjson.GetBytes(body, "tool_choice").String() == "auto" {
		body, _ = sjson.DeleteBytes(body, "tool_choice")
	}
	clone := *request
	clone.Body = body
	if len(request.JSONBody) > 0 {
		clone.JSONBody = body
	}
	return &clone
}

func liftRelayToolDeclarations(body []byte) []byte {
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return body
	}
	var items []json.RawMessage
	var definitions []json.RawMessage
	for _, tool := range gjson.GetBytes(body, "tools").Array() {
		definitions = append(definitions, json.RawMessage(tool.Raw))
	}
	lifted := false
	for _, item := range input.Array() {
		if item.Get("type").String() != "additional_tools" {
			items = append(items, json.RawMessage(item.Raw))
			continue
		}
		if !item.Get("tools").IsArray() {
			return body // Invalid data must still reach normal validation intact.
		}
		lifted = true
		for _, tool := range item.Get("tools").Array() {
			if tool.Get("type").String() == "namespace" && tool.Get("name").String() == "functions" {
				if !tool.Get("tools").IsArray() {
					return body
				}
				for _, child := range tool.Get("tools").Array() {
					definitions = append(definitions, json.RawMessage(child.Raw))
				}
			} else {
				definitions = append(definitions, json.RawMessage(tool.Raw))
			}
		}
	}
	if !lifted {
		return body
	}
	// Later declarations update a definition without changing its position.
	// Keep namespaces intact; flattening arbitrary namespaces changes call names.
	tools := make([]json.RawMessage, 0, len(definitions))
	positions := make(map[string]int, len(definitions))
	for _, tool := range definitions {
		key := gjson.GetBytes(tool, "type").String() + ":" + gjson.GetBytes(tool, "name").String()
		if index, found := positions[key]; found {
			tools[index] = tool
		} else {
			positions[key] = len(tools)
			tools = append(tools, tool)
		}
	}
	for i, item := range items {
		typ := gjson.GetBytes(item, "type").String()
		if (typ == "function_call" || typ == "custom_tool_call") && gjson.GetBytes(item, "namespace").String() == "functions" {
			items[i], _ = sjson.DeleteBytes(item, "namespace")
		}
	}
	rawInput, _ := json.Marshal(items)
	rawTools, _ := json.Marshal(tools)
	result, err := sjson.SetRawBytes(body, "input", rawInput)
	if err != nil {
		return body
	}
	result, err = sjson.SetRawBytes(result, "tools", rawTools)
	if err != nil {
		return body
	}
	// This field selects a private Lite reasoning mode, not reasoning effort.
	result, _ = sjson.DeleteBytes(result, "reasoning.context")
	return result
}
