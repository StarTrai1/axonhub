package responses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// Codex 0.158 serializes numeric custom efforts as u64, not quoted strings.
// Keep that wire type at the Responses boundary and retain shared string-based
// effort mapping. A mapped effort must not restore the original numeric value.
func decodeReasoningEffort(raw json.RawMessage) (string, string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", "", nil
	}
	if raw[0] == '"' {
		var effort string
		err := json.Unmarshal(raw, &effort)
		return effort, "", err
	}
	value, err := strconv.ParseUint(string(raw), 10, 64)
	if err != nil {
		return "", "", fmt.Errorf("reasoning.effort must be a string or unsigned integer: %w", err)
	}
	effort := strconv.FormatUint(value, 10)
	return effort, effort, nil
}

func marshalReasoningEffort(value any, effort, numeric string) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil || numeric == "" || numeric != effort {
		return body, err
	}
	if _, err := strconv.ParseUint(numeric, 10, 64); err != nil {
		return nil, fmt.Errorf("invalid numeric reasoning effort: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	fields["effort"] = json.RawMessage(numeric)
	return json.Marshal(fields)
}

func (r *Reasoning) UnmarshalJSON(data []byte) error {
	type wire Reasoning
	var decoded wire
	payload := struct {
		*wire
		Effort json.RawMessage `json:"effort"`
	}{wire: &decoded}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	effort, numeric, err := decodeReasoningEffort(payload.Effort)
	if err != nil {
		return err
	}
	decoded.Effort, decoded.NumericEffort = effort, numeric
	*r = Reasoning(decoded)
	return nil
}

func (r Reasoning) MarshalJSON() ([]byte, error) {
	type wire Reasoning
	return marshalReasoningEffort(wire(r), r.Effort, r.NumericEffort)
}

func (r *ResponseReasoning) UnmarshalJSON(data []byte) error {
	type wire ResponseReasoning
	var decoded wire
	payload := struct {
		*wire
		Effort json.RawMessage `json:"effort"`
	}{wire: &decoded}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	effort, numeric, err := decodeReasoningEffort(payload.Effort)
	if err != nil {
		return err
	}
	decoded.Effort, decoded.NumericEffort = effort, numeric
	*r = ResponseReasoning(decoded)
	return nil
}

func (r ResponseReasoning) MarshalJSON() ([]byte, error) {
	type wire ResponseReasoning
	return marshalReasoningEffort(wire(r), r.Effort, r.NumericEffort)
}
