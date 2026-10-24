package shared

import (
	"bytes"
	"encoding/json"
)

const ChatImageConfigMetadataKey = "openai_chat_image_config"

// ChatImageConfig also accepts the generic map shape produced by persisted
// metadata. Old records without this optional key retain their original behavior.
func ChatImageConfig(metadata map[string]any) json.RawMessage {
	value, ok := metadata[ChatImageConfigMetadataKey]
	if !ok || value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	return raw
}
