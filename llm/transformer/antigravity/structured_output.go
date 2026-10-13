package antigravity

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

// Cloud Code can return Claude schema-constrained output as an internal tool
// named json_mode. Only the request that enabled this bridge may unwrap it;
// a client's own tool with that name must keep its tool-call semantics.
func claudeStructuredOutput(req *httpclient.Request) bool {
	if req == nil || !strings.Contains(strings.ToLower(gjson.GetBytes(req.Body, "model").String()), "claude") {
		return false
	}
	schema := gjson.GetBytes(req.Body, "request.generationConfig.responseSchema")
	if !schema.IsObject() {
		return false
	}
	for _, tool := range gjson.GetBytes(req.Body, "request.tools").Array() {
		for _, field := range []string{"functionDeclarations", "function_declarations"} {
			for _, declaration := range tool.Get(field).Array() {
				if declaration.Get("name").String() == "json_mode" {
					return false
				}
			}
		}
	}
	return true
}

type structuredOutputState map[int]bool

func (s structuredOutputState) rewrite(body []byte) ([]byte, error) {
	if !json.Valid(body) {
		return body, nil
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil {
		return body, nil
	}
	var candidates []map[string]json.RawMessage
	if err := json.Unmarshal(response["candidates"], &candidates); err != nil {
		return body, nil
	}
	changed := false
	for position, candidate := range candidates {
		index := position
		if raw := candidate["index"]; len(raw) > 0 {
			if err := json.Unmarshal(raw, &index); err != nil {
				return nil, err
			}
		}
		var content map[string]json.RawMessage
		_ = json.Unmarshal(candidate["content"], &content)
		var parts []map[string]json.RawMessage
		_ = json.Unmarshal(content["parts"], &parts)
		converted := make(map[int]bool)
		for n, part := range parts {
			var call struct {
				Name string          `json:"name"`
				Args json.RawMessage `json:"args"`
			}
			if json.Unmarshal(part["functionCall"], &call) != nil || call.Name != "json_mode" {
				continue
			}
			args := call.Args
			if len(args) > 0 && args[0] == '"' {
				var encoded string
				if err := json.Unmarshal(args, &encoded); err != nil {
					return nil, err
				}
				args = []byte(encoded)
			}
			if !json.Valid(args) {
				return nil, fmt.Errorf("Antigravity structured output contains invalid json_mode arguments")
			}
			text, err := json.Marshal(string(args))
			if err != nil {
				return nil, err
			}
			parts[n] = map[string]json.RawMessage{"text": text}
			converted[n] = true
			s[index] = true
		}
		if len(converted) > 0 {
			kept := make([]map[string]json.RawMessage, 0, len(parts))
			for n, part := range parts {
				// Keep signed thought and real tools, but not commentary mixed
				// into the frame containing the structured JSON answer.
				if len(part["text"]) > 0 && string(part["thought"]) != "true" && !converted[n] {
					continue
				}
				kept = append(kept, part)
			}
			content["parts"], _ = json.Marshal(kept)
			candidate["content"], _ = json.Marshal(content)
			changed = true
		}
		if s[index] && string(candidate["finishReason"]) == `"OTHER"` {
			candidate["finishReason"] = json.RawMessage(`"STOP"`)
			changed = true
		}
	}
	if !changed {
		return body, nil
	}
	response["candidates"], _ = json.Marshal(candidates)
	return json.Marshal(response)
}
