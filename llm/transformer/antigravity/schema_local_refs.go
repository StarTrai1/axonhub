package antigravity

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"
)

// Expand known local references before the strict schema cleanup removes their
// definitions. The existing hint fallback handles cycles and unresolved refs.
// Bound copied material and visit keys deterministically to keep cache prefixes
// stable even for a small schema with exponential reference fan-out.
func inlineKnownSchemaRefs(root map[string]any) map[string]any {
	budget := 256 << 10
	var expand func(map[string]any, map[string]bool, int) map[string]any
	expand = func(node map[string]any, active map[string]bool, depth int) map[string]any {
		if depth > 32 {
			return node
		}
		if ref, ok := node["$ref"].(string); ok && !active[ref] {
			if target := localSchemaPointer(root, ref); target != nil {
				encoded, err := json.Marshal(target)
				if err == nil && len(encoded) <= budget {
					budget -= len(encoded)
					active = maps.Clone(active)
					active[ref] = true
					resolved := expand(deepCopy(target), active, depth+1)
					siblings := maps.Clone(node)
					delete(siblings, "$ref")
					if len(siblings) == 0 {
						return resolved
					}
					return map[string]any{"allOf": []any{resolved, expand(siblings, active, depth+1)}}
				}
			}
		}
		return mapSchemaChildren(node, func(child map[string]any) map[string]any {
			return expand(child, active, depth+1)
		}, false)
	}
	return expand(root, make(map[string]bool), 0)
}

func localSchemaPointer(root map[string]any, ref string) map[string]any {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	var value any = root
	for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		node, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = node[part]
	}
	target, _ := value.(map[string]any)
	return target
}

// Name maps contain schema children; literal values such as enum/default do
// not. A property named "$ref" is a property, never a reference itself.
func mapSchemaChildren(schema map[string]any, visit func(map[string]any) map[string]any, definitions bool) map[string]any {
	for _, key := range slices.Sorted(maps.Keys(schema)) {
		value := schema[key]
		switch key {
		case "$defs", "definitions":
			if !definitions {
				continue
			}
			fallthrough
		case "properties", "patternProperties", "dependentSchemas":
			if children, ok := value.(map[string]any); ok {
				for _, name := range slices.Sorted(maps.Keys(children)) {
					if child, ok := children[name].(map[string]any); ok {
						children[name] = visit(child)
					}
				}
			}
		case "items", "additionalItems", "contains", "additionalProperties", "unevaluatedItems", "unevaluatedProperties", "propertyNames", "not", "if", "then", "else":
			if child, ok := value.(map[string]any); ok {
				schema[key] = visit(child)
				continue
			}
			fallthrough
		case "allOf", "anyOf", "oneOf", "prefixItems":
			if children, ok := value.([]any); ok {
				for i, child := range children {
					if node, ok := child.(map[string]any); ok {
						children[i] = visit(node)
					}
				}
			}
		}
	}
	return schema
}
