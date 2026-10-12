package anthropic

import "encoding/json"

const transformerMetadataThinkingExtensions = "anthropic_thinking_extensions"

func (thinking *Thinking) UnmarshalJSON(data []byte) error {
	type fields Thinking
	var known fields
	if err := json.Unmarshal(data, &known); err != nil {
		return err
	}
	var extra map[string]json.RawMessage
	if err := json.Unmarshal(data, &extra); err != nil {
		return err
	}
	for _, key := range []string{"type", "budget_tokens", "display", "block_binding"} {
		delete(extra, key)
	}
	*thinking = Thinking(known)
	if len(extra) > 0 {
		thinking.Extra = extra
	}
	return nil
}

func (thinking Thinking) MarshalJSON() ([]byte, error) {
	type fields Thinking
	data, err := json.Marshal(fields(thinking))
	if err != nil || len(thinking.Extra) == 0 {
		return data, err
	}
	merged := make(map[string]json.RawMessage, len(thinking.Extra)+4)
	for key, value := range thinking.Extra {
		switch key {
		case "type", "budget_tokens", "display", "block_binding":
			continue
		default:
			merged[key] = value
		}
	}
	if err := json.Unmarshal(data, &merged); err != nil {
		return nil, err
	}
	return json.Marshal(merged)
}
