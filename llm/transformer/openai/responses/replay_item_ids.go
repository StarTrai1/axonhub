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
	if request == nil || !bytes.Contains(request.Body, []byte("item_")) {
		return request
	}

	target, err := url.Parse(request.URL)
	if err != nil {
		return request
	}

	switch strings.ToLower(target.Hostname()) {
	case "api.openai.com", "chatgpt.com", "chat.openai.com":
	default:
		return request
	}

	input := gjson.GetBytes(request.Body, "input")
	if !input.IsArray() || !gjson.ValidBytes(request.Body) {
		return request
	}

	body := request.Body
	input.ForEach(func(index, item gjson.Result) bool {
		identifier := item.Get("id")
		if identifier.Type != gjson.String || !strings.HasPrefix(identifier.Str, "item_") || len(identifier.Str) <= len("item_") {
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

	return &cloned
}
