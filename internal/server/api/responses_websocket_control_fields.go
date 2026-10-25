package api

import (
	"strings"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

// Validate before map decoding or lane routing: JSON maps discard duplicate keys,
// while struct decoding also accepts case variants. Control fields must have one
// unambiguous meaning. Nested input/tool payloads remain opaque to this check.
func validateResponsesWebSocketControlFields(message []byte) *httpclient.Error {
	if !gjson.ValidBytes(message) {
		return invalidResponsesWebSocketRequest("invalid Responses WebSocket event JSON payload", "")
	}
	payload := gjson.ParseBytes(message)
	if !payload.IsObject() {
		return invalidResponsesWebSocketRequest("expected a Responses WebSocket event object", "")
	}
	seen := make(map[string]bool)
	var validationErr *httpclient.Error
	payload.ForEach(func(key, _ gjson.Result) bool {
		name := key.String()
		canonical := strings.ToLower(name)
		switch canonical {
		case "type", "stream_id", "response_id", "mode", "input", "previous_response_id":
			if name != canonical || seen[canonical] {
				validationErr = invalidResponsesWebSocketRequest("control fields must use canonical lowercase names and must not be repeated", canonical)
				return false
			}
			seen[canonical] = true
		}
		return true
	})
	return validationErr
}
