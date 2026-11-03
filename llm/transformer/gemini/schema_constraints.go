package gemini

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Gemini's protobuf Schema encodes integer constraints as strings. Standard
// JSON Schema requires numbers. Visit schema positions only: defaults, examples,
// enum values and properties named after schema keywords are application data.
func normalizeGeminiSchema(raw json.RawMessage) json.RawMessage {
	return normalizeGeminiSchemaAt(raw, 0)
}

func normalizeGeminiSchemaAt(raw json.RawMessage, depth int) json.RawMessage {
	if depth > 64 {
		return raw
	}
	var node map[string]json.RawMessage
	if json.Unmarshal(raw, &node) != nil || node == nil {
		return raw
	}
	changed := false
	for key, value := range node {
		switch key {
		case "type":
			var name string
			if json.Unmarshal(value, &name) == nil && name != strings.ToLower(name) {
				node[key], _ = json.Marshal(strings.ToLower(name))
				changed = true
			}
		case "minItems", "maxItems", "minLength", "maxLength", "minProperties", "maxProperties":
			var text string
			if json.Unmarshal(value, &text) == nil {
				integer, err := strconv.ParseInt(text, 10, 64)
				if err == nil && integer >= 0 && strconv.FormatInt(integer, 10) == text {
					node[key] = json.RawMessage(text)
					changed = true
				}
			}
		case "properties", "patternProperties", "dependentSchemas", "dependencies", "$defs", "definitions":
			var children map[string]json.RawMessage
			if json.Unmarshal(value, &children) == nil && children != nil {
				for name, child := range children {
					children[name] = normalizeGeminiSchemaAt(child, depth+1)
				}
				updated, _ := json.Marshal(children)
				node[key] = updated
				changed = true
			}
		case "items", "additionalItems", "additionalProperties", "unevaluatedItems", "unevaluatedProperties", "if", "then", "else", "not", "contains", "propertyNames", "contentSchema", "prefixItems", "allOf", "anyOf", "oneOf":
			var children []json.RawMessage
			if json.Unmarshal(value, &children) == nil && children != nil {
				for i, child := range children {
					children[i] = normalizeGeminiSchemaAt(child, depth+1)
				}
				node[key], _ = json.Marshal(children)
			} else {
				node[key] = normalizeGeminiSchemaAt(value, depth+1)
			}
			changed = changed || string(node[key]) != string(value)
		}
	}
	if !changed {
		return raw
	}
	encoded, err := json.Marshal(node)
	if err != nil {
		return raw
	}
	return encoded
}
