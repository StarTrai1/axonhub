package openai

import (
	"strings"

	"github.com/looplj/axonhub/llm"
)

// chatNamedToolChoiceName resolves a Responses function selection against the
// flat names emitted by the Chat tool catalog. Native Responses keeps its own
// selection unchanged so a later attempt can still use the original dialect.
func chatNamedToolChoiceName(req *llm.Request) string {
	choice := req.ToolChoice.NamedToolChoice
	name := choice.Function.Name
	if req.APIFormat != llm.APIFormatOpenAIResponse || choice.Type != llm.ToolTypeFunction || name == "" {
		return name
	}
	if choice.Function.Namespace != "" {
		return choice.Function.Namespace + "__" + name
	}

	matched := ""
	ambiguous := false
	for _, tool := range req.Tools {
		if tool.Type != llm.ToolTypeFunction {
			continue
		}
		if tool.Function.Name == name {
			// An exact declared name takes precedence over an inferred namespace.
			return name
		}
		if tool.Function.Namespace == "" ||
			strings.TrimPrefix(tool.Function.Name, tool.Function.Namespace+"__") != name {
			continue
		}
		if matched != "" && matched != tool.Function.Name {
			ambiguous = true
		}
		matched = tool.Function.Name
	}
	if matched != "" && !ambiguous {
		return matched
	}
	return name
}
