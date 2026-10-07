package decisions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/looplj/axonhub/llm/transformer"
)

func invalidRequest(reason string) error {
	return fmt.Errorf("%w: %s", transformer.ErrInvalidRequest, reason)
}

// Only interpreted envelope keys are canonicalized. Nested evidence and unknown
// extension fields remain opaque, and the original bytes are forwarded.
func validateEnvelope(body []byte) error {
	if !json.Valid(body) {
		return invalidRequest("Decisions body must be valid JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return invalidRequest("Decisions body must be an object")
	}
	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return invalidRequest("invalid Decisions field")
		}
		key, _ := token.(string)
		for _, canonical := range []string{"model", "input", "questions", "stream"} {
			if strings.EqualFold(key, canonical) {
				if key != canonical || seen[canonical] {
					return invalidRequest("ambiguous Decisions field: " + key)
				}
				seen[canonical] = true
			}
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return invalidRequest("invalid Decisions value")
		}
	}
	return nil
}

func validateRequest(body []byte) (string, error) {
	if err := validateEnvelope(body); err != nil {
		return "", err
	}
	var request struct {
		Model     string            `json:"model"`
		Input     json.RawMessage   `json:"input"`
		Questions []json.RawMessage `json:"questions"`
		Stream    bool              `json:"stream"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return "", invalidRequest("invalid Decisions request fields")
	}
	if strings.TrimSpace(request.Model) == "" {
		return "", invalidRequest("model is required")
	}
	if request.Stream {
		return "", invalidRequest("Decisions does not support streaming")
	}
	if err := validateInput(request.Input); err != nil {
		return "", err
	}
	if len(request.Questions) == 0 {
		return "", invalidRequest("questions must be a nonempty array")
	}
	for _, raw := range request.Questions {
		if err := validateQuestion(raw); err != nil {
			return "", err
		}
	}
	return request.Model, nil
}

func validateInput(raw json.RawMessage) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return invalidRequest("input is required")
	}
	if raw[0] == '"' {
		return nil
	}
	var messages []struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type     string          `json:"type"`
			Text     *string         `json:"text"`
			ImageURL string          `json:"image_url"`
			FileID   json.RawMessage `json:"file_id"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &messages); err != nil || len(messages) == 0 {
		return invalidRequest("input must be text or user messages")
	}
	images := 0
	for _, message := range messages {
		if message.Role != "user" || (message.Type != "" && message.Type != "message") || len(message.Content) == 0 {
			return invalidRequest("Decisions only accepts user messages with text or images")
		}
		for _, part := range message.Content {
			switch part.Type {
			case "input_text":
				if part.Text == nil {
					return invalidRequest("input_text requires text")
				}
			case "input_image":
				prefix, data, ok := strings.Cut(part.ImageURL, ",")
				if !ok || !strings.HasPrefix(prefix, "data:image/") || !strings.HasSuffix(prefix, ";base64") || data == "" || len(part.FileID) != 0 {
					return invalidRequest("Decisions images require inline base64 data URLs without file_id")
				}
				images++
			default:
				return invalidRequest("unsupported Decisions input type")
			}
		}
	}
	if images > 128 {
		return invalidRequest("Decisions accepts at most 128 images")
	}
	return nil
}

func validateQuestion(raw json.RawMessage) error {
	var question struct {
		Type         string            `json:"type"`
		Name         *string           `json:"name"`
		Instructions string            `json:"instructions"`
		Choices      []json.RawMessage `json:"choices"`
		Levels       []json.RawMessage `json:"levels"`
	}
	if err := json.Unmarshal(raw, &question); err != nil || strings.TrimSpace(question.Instructions) == "" {
		return invalidRequest("questions require instructions")
	}
	switch question.Type {
	case "predicate":
		return nil
	case "choice":
		if len(question.Choices) == 0 {
			return invalidRequest("choice questions require choices")
		}
	case "score":
		if len(question.Levels) == 0 {
			return invalidRequest("score questions require levels")
		}
	default:
		return invalidRequest("unsupported Decisions question type")
	}
	return nil
}
