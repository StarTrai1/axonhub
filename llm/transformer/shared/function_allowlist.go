package shared

import (
	"fmt"
	"strings"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer"
)

// RestrictFunctionAllowlist translates a function-only allowed_tools choice for
// providers whose tool-choice modes do not carry a separate allowlist. Native
// Responses callers must keep their original choice instead of using this helper.
func RestrictFunctionAllowlist(req *llm.Request) (*llm.Request, error) {
	if req == nil || req.ToolChoice == nil || req.ToolChoice.NamedToolChoice == nil ||
		req.ToolChoice.NamedToolChoice.Type != "allowed_tools" {
		return req, nil
	}
	choice := req.ToolChoice
	if choice.ToolChoice == nil || (*choice.ToolChoice != "auto" && *choice.ToolChoice != "required") || len(choice.Tools) == 0 {
		return nil, fmt.Errorf("%w: allowed_tools requires auto/required mode and a nonempty function list", transformer.ErrInvalidRequest)
	}
	allowed := make(map[string]bool, len(choice.Tools))
	for _, option := range choice.Tools {
		if option.Type != llm.ToolTypeFunction || strings.TrimSpace(option.Name) == "" || allowed[option.Name] {
			return nil, fmt.Errorf("%w: allowed_tools requires distinct named functions for this provider", transformer.ErrInvalidRequest)
		}
		allowed[option.Name] = true
	}
	selected := make([]llm.Tool, 0, len(allowed))
	found := make(map[string]bool, len(allowed))
	for _, tool := range req.Tools {
		if tool.Type != llm.ToolTypeFunction || !allowed[tool.Function.Name] {
			continue
		}
		if found[tool.Function.Name] || tool.Function.Namespace != "" {
			return nil, fmt.Errorf("%w: allowed_tools function declarations must be unambiguous for this provider", transformer.ErrInvalidRequest)
		}
		found[tool.Function.Name] = true
		selected = append(selected, tool)
	}
	if len(found) != len(allowed) {
		return nil, fmt.Errorf("%w: allowed_tools references an undeclared function", transformer.ErrInvalidRequest)
	}
	adapted := *req
	adapted.Tools = selected
	adapted.ToolChoice = &llm.ToolChoice{ToolChoice: choice.ToolChoice}
	return &adapted, nil
}
