package responses

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

func PrepareReplayItemIDs(request *httpclient.Request) *httpclient.Request {
	if request == nil || (!bytes.Contains(request.Body, []byte("item_")) && !bytes.Contains(request.Body, []byte("call_"))) {
		return request
	}

	target, err := url.Parse(request.URL)
	if err != nil {
		return request
	}

	stripSynthetic := false
	switch strings.ToLower(target.Hostname()) {
	case "api.openai.com", "chatgpt.com", "chat.openai.com":
		stripSynthetic = true
	}

	input := gjson.GetBytes(request.Body, "input")
	if !input.IsArray() || !gjson.ValidBytes(request.Body) {
		return request
	}

	body := request.Body
	references := make(map[string]bool)
	for _, item := range input.Array() {
		if item.Get("type").String() == "item_reference" {
			references[item.Get("id").String()] = true
		}
	}
	input.ForEach(func(index, item gjson.Result) bool {
		identifier := item.Get("id")
		if identifier.Type != gjson.String {
			return true
		}
		// Earlier converted responses reused call_id as custom-tool item ID.
		// Repair persisted history on compatible relays as well as native hosts,
		// but only for fully materialized calls with this exact legacy shape.
		legacyCustom := item.Get("type").String() == "custom_tool_call" &&
			strings.HasPrefix(identifier.Str, "call_") && len(identifier.Str) > len("call_") &&
			identifier.Str == item.Get("call_id").String() && item.Get("name").String() != "" &&
			item.Get("input").Type == gjson.String && !references[identifier.Str]
		if !legacyCustom && (!stripSynthetic || !strings.HasPrefix(identifier.Str, "item_") || len(identifier.Str) <= len("item_")) {
			return true
		}

		switch item.Get("type").String() {
		case "message", "reasoning", "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output":
		default:
			return true
		}

		body, err = sjson.DeleteBytes(body, fmt.Sprintf("input.%d.id", index.Int()))
		return err == nil
	})
	if err != nil || bytes.Equal(body, request.Body) {
		return request
	}

	cloned := *request
	cloned.Body = body
	if len(cloned.JSONBody) > 0 {
		cloned.JSONBody = body
	}

	return &cloned
}
