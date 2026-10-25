package api

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/httpclient"
)

// routeInterrupt binds a Codex interrupt to a response on this connection.
// A late interrupt for a recently finished response receives an idempotent acknowledgement.
func (d *responsesWebSocketDispatcher) routeInterrupt(message []byte) *httpclient.Error {
	if err := validateResponsesWebSocketControlFields(message); err != nil {
		return err
	}
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
	terminal, routeErr := d.queueInterrupt(payload.ResponseID, message)
	if routeErr != nil {
		return routeErr
	}
	if terminal {
		if err := d.acknowledgeInterrupt(payload.ResponseID); err != nil {
			return responsesWebSocketRequestError("failed to acknowledge interruption", "response_id", "interrupt_not_available")
		}
	}
	return nil
}

func (d *responsesWebSocketDispatcher) queueInterrupt(responseID string, message []byte) (bool, *httpclient.Error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	lane := d.responseLanes[responseID]
	if lane == nil {
		if d.terminalResponses.Contains(responseID) {
			return true, nil
		}
		return false, responsesWebSocketRequestError("response not found on this WebSocket connection", "response_id", "response_not_found")
	}
	lane.mu.Lock()
	defer lane.mu.Unlock()
	if !lane.active {
		if d.terminalResponses.Contains(responseID) {
			return true, nil
		}
		return false, responsesWebSocketRequestError("response not found on this WebSocket connection", "response_id", "response_not_found")
	}
	if lane.steer.IsTerminalResponse(responseID) {
		return true, nil
	}
	if !lane.steer.Ready() {
		if lane.steer.IsTerminalResponse(responseID) {
			return true, nil
		}
		return false, responsesWebSocketRequestError("response.interrupt requires an active upstream Responses WebSocket", "response_id", "interrupt_not_supported")
	}
	if !lane.steer.Send(append([]byte(nil), message...)) {
		if lane.steer.IsTerminalResponse(responseID) {
			return true, nil
		}
		return false, responsesWebSocketRequestError("Responses WebSocket control queue is full or no longer active", "response_id", "interrupt_not_available")
	}
	if d.pendingInterrupts == nil {
		d.pendingInterrupts = make(map[string]struct{})
	}
	d.pendingInterrupts[responseID] = struct{}{}
	return false, nil
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

func (d *responsesWebSocketDispatcher) acknowledgeInterrupt(responseID string) error {
	return writeResponsesWebSocketJSON(d.writer, map[string]string{"type": "response.interrupt.accepted", "response_id": responseID})
}

func (d *responsesWebSocketDispatcher) observeInterruptEvent(event *httpclient.StreamEvent) error {
	typ := event.Type
	if typ == "" {
		typ = gjson.GetBytes(event.Data, "type").String()
	}
	acknowledgement := typ == "response.interrupt.accepted" || typ == "response.interrupt.failed"
	id := gjson.GetBytes(event.Data, "response_id").String()
	if !acknowledgement {
		switch typ {
		case "response.completed", "response.incomplete", "response.failed", "response.cancelled":
			id = responseIDFromWebSocketEvent(event.Data)
		default:
			return nil
		}
	}
	d.mu.Lock()
	_, pending := d.pendingInterrupts[id]
	delete(d.pendingInterrupts, id)
	d.mu.Unlock()
	if pending && !acknowledgement {
		return d.acknowledgeInterrupt(id)
	}
	return nil
}
