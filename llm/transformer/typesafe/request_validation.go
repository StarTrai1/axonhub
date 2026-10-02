package typesafe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/looplj/axonhub/llm/transformer"
)

// The body is already valid JSON. Reject ambiguous spellings of the envelope
// fields we interpret before forwarding the raw request. In particular,
// encoding/json reads stream:true,Stream:false as false, but an upstream may
// read the canonical stream field as true. State and question data stay opaque.
func validateSystemOneEnvelopeKeys(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return fmt.Errorf("%w: systemone request must be an object", transformer.ErrInvalidRequest)
	}
	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return fmt.Errorf("%w: invalid systemone request key", transformer.ErrInvalidRequest)
		}
		key, _ := token.(string)
		for _, canonical := range []string{"model", "state", "questions", "stream"} {
			if !strings.EqualFold(key, canonical) {
				continue
			}
			if key != canonical || seen[canonical] {
				return fmt.Errorf("%w: ambiguous systemone request field %q", transformer.ErrInvalidRequest, key)
			}
			seen[canonical] = true
			break
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return fmt.Errorf("%w: invalid systemone request value", transformer.ErrInvalidRequest)
		}
	}
	return nil
}
