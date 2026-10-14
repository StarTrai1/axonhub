package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/looplj/axonhub/llm/httpclient"
)

// Based on CLIProxyAPI 57d005fe: Codex rejects integer numeric bounds outside int64.
// Only schema keywords are changed; examples, defaults, enum values and tool inputs remain opaque.
func normalizeNumericToolBounds(request *httpclient.Request) *httpclient.Request {
	if request == nil || (!bytes.Contains(request.Body, []byte("inimum")) && !bytes.Contains(request.Body, []byte("aximum"))) {
		return request
	}
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(request.Body))
	decoder.UseNumber()
	if decoder.Decode(&payload) != nil {
		return request
	}
	changed := normalizeToolBoundList(payload["tools"])
	if input, ok := payload["input"].([]any); ok {
		for _, value := range input {
			item, ok := value.(map[string]any)
			if !ok {
				continue
			}
			if item["type"] == "additional_tools" || item["type"] == "tool_search_output" {
				if normalizeToolBoundList(item["tools"]) {
					changed = true
				}
			}
		}
	}
	if !changed {
		return request
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return request
	}
	cloned := *request
	cloned.Body = body
	return &cloned
}

func normalizeToolBoundList(value any) bool {
	tools, ok := value.([]any)
	if !ok {
		return false
	}
	changed := false
	for _, value := range tools {
		tool, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if tool["type"] == "namespace" {
			if normalizeToolBoundList(tool["tools"]) {
				changed = true
			}
		}
		if tool["type"] != "function" {
			continue
		}
		if nested, ok := tool["function"].(map[string]any); ok {
			tool = nested
		}
		if schema, ok := tool["parameters"].(map[string]any); ok && normalizeSchemaBounds(schema) {
			changed = true
		}
	}
	return changed
}

func normalizeSchemaBounds(schema map[string]any) bool {
	changed := false
	for _, keyword := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum"} {
		if number, ok := schema[keyword].(json.Number); ok && isNumericBoundOutOfInt64(number) {
			delete(schema, keyword)
			changed = true
		}
	}
	for _, key := range []string{"properties", "patternProperties", "$defs", "definitions", "dependentSchemas"} {
		if children, ok := schema[key].(map[string]any); ok {
			for _, value := range children {
				if child, ok := value.(map[string]any); ok && normalizeSchemaBounds(child) {
					changed = true
				}
			}
		}
	}
	for _, key := range []string{"items", "prefixItems", "allOf", "anyOf", "oneOf", "additionalProperties", "additionalItems", "contains", "not", "if", "then", "else", "propertyNames", "unevaluatedProperties", "unevaluatedItems", "contentSchema"} {
		switch value := schema[key].(type) {
		case map[string]any:
			if normalizeSchemaBounds(value) {
				changed = true
			}
		case []any:
			for _, element := range value {
				if child, ok := element.(map[string]any); ok && normalizeSchemaBounds(child) {
					changed = true
				}
			}
		}
	}
	return changed
}

func isNumericBoundOutOfInt64(number json.Number) bool {
	raw := strings.TrimSpace(number.String())
	if raw == "" {
		return false
	}
	if !strings.ContainsAny(raw, ".eE") {
		_, errParse := strconv.ParseInt(raw, 10, 64)
		return errors.Is(errParse, strconv.ErrRange)
	}

	s := raw
	negative := false
	if strings.HasPrefix(s, "-") {
		negative = true
		s = s[1:]
	} else if strings.HasPrefix(s, "+") {
		s = s[1:]
	}

	mantissa := s
	var exp int64
	if idx := strings.IndexAny(s, "eE"); idx >= 0 {
		mantissa = s[:idx]
		expStr := s[idx+1:]
		parsedExp, errExp := strconv.ParseInt(expStr, 10, 64)
		if errExp != nil {
			if errors.Is(errExp, strconv.ErrRange) {
				return !strings.HasPrefix(expStr, "-") && strings.Trim(mantissa, "0.") != ""
			}
			return false
		}
		exp = parsedExp
	}

	intPart := mantissa
	fracPart := ""
	if dotIdx := strings.IndexByte(mantissa, '.'); dotIdx >= 0 {
		intPart = mantissa[:dotIdx]
		fracPart = mantissa[dotIdx+1:]
		if strings.IndexByte(fracPart, '.') >= 0 {
			return false
		}
	}
	if intPart == "" && fracPart == "" {
		return false
	}
	for i := range len(intPart) {
		if intPart[i] < '0' || intPart[i] > '9' {
			return false
		}
	}
	for i := range len(fracPart) {
		if fracPart[i] < '0' || fracPart[i] > '9' {
			return false
		}
	}

	allDigits := intPart + fracPart
	sig := strings.TrimLeft(allDigits, "0")
	if sig == "" {
		return false
	}
	if exp < -int64(len(raw)) {
		return false
	}
	if exp > int64(len(raw))+20 {
		return true
	}
	trimmed := strings.TrimRight(sig, "0")
	trailingZeros := int64(len(sig) - len(trimmed))
	effectiveExp := exp - int64(len(fracPart)) + trailingZeros
	if effectiveExp < 0 {
		return false
	}
	if effectiveExp > 19 || int64(len(trimmed))+effectiveExp > 19 {
		return true
	}

	intStr := trimmed + strings.Repeat("0", int(effectiveExp))
	if negative {
		intStr = "-" + intStr
	}
	_, errParse := strconv.ParseInt(intStr, 10, 64)
	return errors.Is(errParse, strconv.ErrRange)
}
