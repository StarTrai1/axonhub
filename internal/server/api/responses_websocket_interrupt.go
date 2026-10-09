package api

import (
	"encoding/json"
	"strings"

	"github.com/looplj/axonhub/llm/httpclient"
)

// routeInterrupt binds a Codex interrupt to a response on this connection.
// A late interrupt for a recently finished response is an idempotent no-op.
func (d *responsesWebSocketDispatcher) routeInterrupt(message []byte) *httpclient.Error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(message, &raw); err != nil || raw == nil {
		return invalidResponsesWebSocketRequest("invalid response.interrupt JSON payload", "")
	}
	for key := range raw {
		switch key {
		case "type", "response_id", "mode":
		default:
			return invalidResponsesWebSocketRequest("response.interrupt accepts only type, response_id, and mode", key)
		}
	}
	var payload struct {
		Type       string `json:"type"`
		ResponseID string `json:"response_id"`
		Mode       string `json:"mode"`
	}
	if json.Unmarshal(message, &payload) != nil || payload.Type != responseInterruptWebSocketEventType || strings.TrimSpace(payload.ResponseID) == "" {
		return invalidResponsesWebSocketRequest("response.interrupt requires a response_id", "response_id")
	}
	if payload.Mode != "discard_partial_items" {
		return invalidResponsesWebSocketRequest("response.interrupt requires mode discard_partial_items", "mode")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	lane := d.responseLanes[payload.ResponseID]
	if lane == nil {
		if d.terminalResponses.Contains(payload.ResponseID) {
			return nil
		}
		return responsesWebSocketRequestError("response not found on this WebSocket connection", "response_id", "response_not_found")
	}
	lane.mu.Lock()
	defer lane.mu.Unlock()
	if !lane.active {
		if d.terminalResponses.Contains(payload.ResponseID) {
			return nil
		}
		return responsesWebSocketRequestError("response not found on this WebSocket connection", "response_id", "response_not_found")
	}
	if lane.steer.IsTerminalResponse(payload.ResponseID) {
		return nil
	}
	if !lane.steer.Ready() {
		if lane.steer.IsTerminalResponse(payload.ResponseID) {
			return nil
		}
		return responsesWebSocketRequestError("response.interrupt requires an active upstream Responses WebSocket", "response_id", "interrupt_not_supported")
	}
	if !lane.steer.Send(append([]byte(nil), message...)) {
		if lane.steer.IsTerminalResponse(payload.ResponseID) {
			return nil
		}
		return responsesWebSocketRequestError("Responses WebSocket control queue is full or no longer active", "response_id", "interrupt_not_available")
	}
	return nil
}

func isInterruptedResponsesTerminal(event *httpclient.StreamEvent) bool {
	if event == nil || event.Type != "response.incomplete" {
		return false
	}
	var payload struct {
		Response struct {
			IncompleteDetails struct {
				Reason string `json:"reason"`
			} `json:"incomplete_details"`
		} `json:"response"`
	}
	return json.Unmarshal(event.Data, &payload) == nil && payload.Response.IncompleteDetails.Reason == "interrupted"
}
