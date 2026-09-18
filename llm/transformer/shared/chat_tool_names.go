package shared

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"

	"github.com/looplj/axonhub/llm"
)

const (
	ChatToolAliasesMetadataKey   = "responses_chat_tool_aliases_v1"
	ChatToolOriginalsMetadataKey = "responses_chat_tool_originals_v1"
)

// ResponsesChatToolAliases assigns bounded Chat names without changing native
// Responses identities. Reserve short declarations, local names and history
// first so a generated alias can never take another tool's existing name.
func ResponsesChatToolAliases(req *llm.Request) map[string]string {
	reserved := make(map[string]bool)
	long := make(map[string]bool)
	add := func(name string) {
		reserved[name] = true
		if len(name) > 64 {
			long[name] = true
		}
	}
	for _, tool := range req.Tools {
		if tool.Type == llm.ToolTypeFunction {
			add(tool.Function.Name)
			reserved[strings.TrimPrefix(tool.Function.Name, tool.Function.Namespace+"__")] = true
		}
	}
	for _, message := range req.Messages {
		for _, call := range message.ToolCalls {
			if call.Type == llm.ToolTypeFunction || call.Type == "" {
				add(call.Function.Name)
			}
		}
	}
	if req.ToolChoice != nil && req.ToolChoice.NamedToolChoice != nil {
		choice := req.ToolChoice.NamedToolChoice
		if choice.Type == llm.ToolTypeFunction {
			name := choice.Function.Name
			if choice.Function.Namespace != "" {
				name = choice.Function.Namespace + "__" + name
			}
			add(name)
		}
	}
	if len(long) == 0 {
		return nil
	}
	names := make([]string, 0, len(long))
	for name := range long {
		names = append(names, name)
	}
	slices.Sort(names)
	aliases := make(map[string]string, len(names))
	for _, name := range names {
		tail := strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
				return r
			}
			return '_'
		}, name)
		if len(tail) > 42 {
			tail = tail[len(tail)-42:]
		}
		for salt := 0; ; salt++ {
			digest := sha256.Sum256(fmt.Appendf(nil, "%s\x00%d", name, salt))
			alias := fmt.Sprintf("tool_%x_%s", digest[:8], tail)
			if !reserved[alias] {
				aliases[name] = alias
				reserved[alias] = true
				break
			}
		}
	}
	return aliases
}

// MappedChatToolName accepts live metadata and metadata restored through JSON.
// Absent mappings leave names untouched, including native Chat requests.
func MappedChatToolName(metadata map[string]any, key, name string) string {
	var mapped string
	switch values := metadata[key].(type) {
	case map[string]string:
		mapped = values[name]
	case map[string]any:
		mapped, _ = values[name].(string)
	}
	if mapped != "" {
		return mapped
	}
	return name
}
