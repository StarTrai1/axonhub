package codex

import (
	"bytes"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

const historyWebSearchTool = `{"type":"web_search","external_web_access":false}`

// A replayed hosted search requires a declaration on the official Codex
// endpoint, including local summarization requests that otherwise have no tools.
// Run after pass-through selection so HTTP and WebSocket send the same repair.
func (t *OutboundTransformer) normalizeWebSearchHistory(request *httpclient.Request) *httpclient.Request {
	if t == nil || !t.isOfficialCodex() || request == nil ||
		request.RequestType == llm.RequestTypeCompact.String() ||
		request.APIFormat == string(llm.APIFormatOpenAIResponseCompact) {
		return request
	}
	if !bytes.Contains(request.Body, []byte(`"web_search_call"`)) {
		return request
	}
	input := gjson.GetBytes(request.Body, "input")
	if !input.IsArray() {
		return request
	}
	tools := gjson.GetBytes(request.Body, "tools")
	if tools.Exists() && tools.Type != gjson.Null && !tools.IsArray() {
		return request
	}
	if containsHistoryWebSearchTool(tools) {
		return request
	}
	items := input.Array()
	hasHistory := false
	callerHasTools := len(tools.Array()) > 0
	for _, item := range items {
		switch item.Get("type").String() {
		case "web_search_call":
			hasHistory = true
		case "additional_tools":
			catalog := item.Get("tools")
			if catalog.Exists() && catalog.Type != gjson.Null && !catalog.IsArray() {
				return request
			}
			if containsHistoryWebSearchTool(catalog) {
				return request
			}
			callerHasTools = callerHasTools || len(catalog.Array()) > 0
		}
	}
	if !hasHistory {
		return request
	}
	if !callerHasTools {
		choice := gjson.GetBytes(request.Body, "tool_choice")
		// Do not turn a required or named choice with no declared tools into
		// permission to invoke the compatibility declaration.
		if choice.Type != gjson.Null && (choice.Type != gjson.String ||
			(choice.String() != "" && choice.String() != "auto" && choice.String() != "none")) {
			return request
		}
	}

	body := request.Body
	var err error
	if strings.EqualFold(strings.TrimSpace(request.Headers.Get(responses.ResponsesLiteHeader)), "true") {
		// Existing catalog IDs describe immutable history. Add a new delta
		// rather than changing an earlier declaration under the same ID.
		at := len(items)
		if at > 0 && items[at-1].Get("type").String() == "compaction_trigger" {
			at--
		}
		parts := make([][]byte, 0, len(items)+1)
		for i := 0; i <= len(items); i++ {
			if i == at {
				parts = append(parts, []byte(`{"type":"additional_tools","role":"developer","tools":[`+historyWebSearchTool+`]}`))
			}
			if i < len(items) {
				parts = append(parts, []byte(items[i].Raw))
			}
		}
		inputBody := append([]byte{'['}, bytes.Join(parts, []byte{','})...)
		inputBody = append(inputBody, ']')
		body, err = sjson.SetRawBytes(body, "input", inputBody)
	} else if tools.IsArray() {
		body, err = sjson.SetRawBytes(body, "tools.-1", []byte(historyWebSearchTool))
	} else {
		body, err = sjson.SetRawBytes(body, "tools", []byte(`[`+historyWebSearchTool+`]`))
	}
	if err != nil {
		return request
	}
	if !callerHasTools {
		body, err = sjson.SetBytes(body, "tool_choice", "none")
		if err != nil {
			return request
		}
	}
	cloned := *request
	cloned.Body = body
	return &cloned
}

func containsHistoryWebSearchTool(tools gjson.Result) bool {
	for _, tool := range tools.Array() {
		if strings.HasPrefix(tool.Get("type").String(), "web_search") {
			return true
		}
	}
	return false
}
