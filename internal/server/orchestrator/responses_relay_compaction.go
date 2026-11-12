package orchestrator

import (
	"bytes"
	"encoding/json"

	"github.com/tidwall/gjson"
)

// Some relays report the client input index even after additional_tools has
// been promoted out of the input array. Accept that coordinate only when the
// original checkpoint matches and removing declarations explains the complete
// prefix. The caller already authenticated its owner, thread, ID and ciphertext.
func responsesRelayCompactionClientIndex(original, provider []byte, ref *remoteCompactionReference, message, param string) bool {
	const code = "unsupported_encrypted_compaction"
	if !responsesRejectedCompactionMessage(original, ref, code, message, param) {
		return false
	}
	current := gjson.GetBytes(provider, "input").Array()
	index, removed := 0, false
	for _, item := range gjson.GetBytes(original, "input").Array() {
		if item.Get("type").String() == "additional_tools" {
			removed = true
			continue
		}
		if index >= len(current) || !equalRelayCompactionPrefixItem(item.Raw, current[index].Raw) {
			return false
		}
		if item.Get("id").String() == ref.ID {
			return removed
		}
		index++
	}
	return false
}

// Canonical JSON comparison changes no request bytes and preserves large JSON
// numbers. Neither item IDs, ciphertext nor tool arguments are ignored.
func equalRelayCompactionPrefixItem(left, right string) bool {
	canonical := func(raw string) ([]byte, error) {
		decoder := json.NewDecoder(bytes.NewBufferString(raw))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		return json.Marshal(value)
	}
	a, err := canonical(left)
	if err != nil {
		return false
	}
	b, err := canonical(right)
	return err == nil && bytes.Equal(a, b)
}
