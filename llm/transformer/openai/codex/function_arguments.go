package codex

import (
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

// Normalize only blank function arguments. Custom tools and malformed nonempty
// arguments retain their original meaning and provider validation behavior.
func normalizeEmptyFunctionArguments(request *httpclient.Request) *httpclient.Request {
	if request == nil {
		return request
	}
	body := request.Body
	changed := false
	for index, item := range gjson.GetBytes(body, "input").Array() {
		arguments := item.Get("arguments")
		if item.Get("type").String() != "function_call" || arguments.Type != gjson.String || strings.TrimSpace(arguments.String()) != "" {
			continue
		}
		updated, err := sjson.SetBytes(body, "input."+strconv.Itoa(index)+".arguments", "{}")
		if err != nil {
			return request
		}
		body = updated
		changed = true
	}
	if !changed {
		return request
	}
	copyRequest := *request
	copyRequest.Body = body
	return &copyRequest
}
