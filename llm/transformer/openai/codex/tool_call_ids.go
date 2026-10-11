package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

// Chat history may carry IDs longer than Codex's 64-character limit. Map
// calls and outputs together on the converted request, without mutating the
// shared history. Native Responses IDs can belong to opaque provider state
// and must never pass through this conversion.
func shortenChatToolCallIDs(request *httpclient.Request) (*httpclient.Request, error) {
	items := gjson.GetBytes(request.Body, "input").Array()
	used := make(map[string]bool)
	for _, item := range items {
		if isChatToolItem(item) {
			used[item.Get("call_id").String()] = true
		}
	}

	mapped := make(map[string]string)
	body := request.Body
	for index, item := range items {
		if !isChatToolItem(item) {
			continue
		}
		id := item.Get("call_id").String()
		if len(id) <= 64 {
			continue
		}
		shortID, ok := mapped[id]
		if !ok {
			for attempt := 0; ; attempt++ {
				seed := id
				if attempt > 0 {
					seed += "_" + strconv.Itoa(attempt)
				}
				digest := sha256.Sum256([]byte(seed))
				shortID = "call_" + hex.EncodeToString(digest[:24])
				if !used[shortID] {
					break
				}
			}
			mapped[id] = shortID
			used[shortID] = true
		}
		updated, err := sjson.SetBytes(body, "input."+strconv.Itoa(index)+".call_id", shortID)
		if err != nil {
			return nil, fmt.Errorf("shorten Codex chat tool call ID: %w", err)
		}
		body = updated
	}
	if len(mapped) == 0 {
		return request, nil
	}
	copyRequest := *request
	copyRequest.Body = body
	return &copyRequest, nil
}

func isChatToolItem(item gjson.Result) bool {
	switch item.Get("type").String() {
	case "function_call", "function_call_output", "custom_tool_call", "custom_tool_call_output":
		return item.Get("call_id").Type == gjson.String
	default:
		return false
	}
}
