package shared

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
)

// CodexRequestMetadata is the stable identity carried by Codex HTTP headers or
// per-request client_metadata. SessionID can be shared by independent threads.
type CodexRequestMetadata struct {
	SessionID       string
	ThreadID        string
	WindowID        string
	RequestKind     string
	Subagent        string
	RawTurnMetadata string
}

type codexTurnIdentity struct {
	SessionID   string `json:"session_id"`
	ThreadID    string `json:"thread_id"`
	WindowID    string `json:"window_id"`
	RequestKind string `json:"request_kind"`
}

// ReadCodexRequestMetadata preserves the original turn metadata while reading
// only identity fields. It does not decode large input or tool-result payloads.
func ReadCodexRequestMetadata(headers http.Header, body []byte) CodexRequestMetadata {
	var envelope struct {
		ClientMetadata map[string]json.RawMessage `json:"client_metadata"`
	}
	_ = json.Unmarshal(body, &envelope)
	clientString := func(key string) string {
		var value string
		_ = json.Unmarshal(envelope.ClientMetadata[key], &value)
		return value
	}

	rawHeader := headers.Get("X-Codex-Turn-Metadata")
	rawBody := clientString("x-codex-turn-metadata")
	var headerTurn, bodyTurn codexTurnIdentity
	if json.Unmarshal([]byte(rawHeader), &headerTurn) != nil {
		headerTurn = codexTurnIdentity{}
	}
	if json.Unmarshal([]byte(rawBody), &bodyTurn) != nil {
		bodyTurn = codexTurnIdentity{}
		rawBody = ""
	} else if strings.ContainsAny(rawBody, "\r\n\t") {
		// JSON whitespace is valid in a body but not in an HTTP field value.
		// Compact only the promoted header; the request body remains untouched.
		var compacted bytes.Buffer
		if json.Compact(&compacted, []byte(rawBody)) == nil {
			rawBody = compacted.String()
		}
	}

	first := func(values ...string) string {
		for _, value := range values {
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
		return ""
	}
	windowID := first(headers.Get("X-Codex-Window-Id"), headerTurn.WindowID, bodyTurn.WindowID)
	windowThreadID, _, _ := strings.Cut(windowID, ":")
	metadata := CodexRequestMetadata{
		SessionID: first(headers.Get("Session_id"), headers.Get("Session-Id"), headerTurn.SessionID, bodyTurn.SessionID),
		ThreadID:  first(
			headers.Get("Thread-Id"),
			headerTurn.ThreadID,
			clientString("thread_id"),
			bodyTurn.ThreadID,
			windowThreadID,
		),
		WindowID:        windowID,
		RequestKind:     first(headerTurn.RequestKind, bodyTurn.RequestKind),
		Subagent:        first(headers.Get("X-Openai-Subagent"), clientString("x-openai-subagent")),
		RawTurnMetadata: rawHeader,
	}
	if metadata.RawTurnMetadata == "" {
		metadata.RawTurnMetadata = rawBody
	}
	return metadata
}
