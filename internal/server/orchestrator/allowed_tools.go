package orchestrator

import (
	"fmt"
	"strings"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer"
)

// applyAllowedToolsForOutbound preserves function restrictions when a Responses
// request is routed to a different protocol. Run this before filtering history
// and before any Chat-based provider transformer can reduce the choice to a mode.
// Native Responses keeps its full catalog and choice for prompt-cache fidelity.
// Native Chat also retains the nested allowed_tools shape supported by its wire.
func applyAllowedToolsForOutbound(req *llm.Request, format llm.APIFormat) (*llm.Request, error) {
	if req == nil || req.ToolChoice == nil || req.ToolChoice.NamedToolChoice == nil ||
		req.ToolChoice.NamedToolChoice.Type != "allowed_tools" || isResponsesFormat(format) {
		return req, nil
	}
	if req.APIFormat == llm.APIFormatOpenAIChatCompletion && format == llm.APIFormatOpenAIChatCompletion {
		return req, nil
	}

	switch format {
	case llm.APIFormatOpenAIChatCompletion, llm.APIFormatAnthropicMessage, llm.APIFormatGeminiContents:
	default:
		return nil, fmt.Errorf("%w: allowed_tools cannot be represented by %s", transformer.ErrInvalidRequest, format)
	}

	choice := req.ToolChoice
	if choice.ToolChoice == nil || (*choice.ToolChoice != "auto" && *choice.ToolChoice != "required") || len(choice.Tools) == 0 {
		return nil, fmt.Errorf("%w: allowed_tools requires auto or required mode and a nonempty function list", transformer.ErrInvalidRequest)
	}
	if err := validateAllowedToolsHistory(req); err != nil {
		return nil, err
	}

	selected := make(map[int]struct{}, len(choice.Tools))
	for _, option := range choice.Tools {
		if option.Type != llm.ToolTypeFunction || strings.TrimSpace(option.Name) == "" {
			return nil, fmt.Errorf("%w: allowed_tools conversion requires named function tools", transformer.ErrInvalidRequest)
		}
		index := allowedFunctionIndex(req.Tools, option.Name)
		if index < 0 {
			return nil, fmt.Errorf("%w: allowed_tools function %q must resolve to one declared function", transformer.ErrInvalidRequest, option.Name)
		}
		if _, duplicate := selected[index]; duplicate {
			return nil, fmt.Errorf("%w: duplicate allowed_tools function %q", transformer.ErrInvalidRequest, option.Name)
		}
		selected[index] = struct{}{}
	}

	cloned := *req
	cloned.Tools = make([]llm.Tool, 0, len(selected))
	for index, tool := range req.Tools {
		if _, allowed := selected[index]; allowed {
			cloned.Tools = append(cloned.Tools, tool)
		}
	}
	cloned.ToolChoice = &llm.ToolChoice{ToolChoice: choice.ToolChoice}
	return &cloned, nil
}

// Prefer an exact flat name. Only infer a namespace when the local name resolves
// uniquely; duplicate declarations and ambiguous local names must not widen access.
func allowedFunctionIndex(tools []llm.Tool, name string) int {
	for _, exact := range []bool{true, false} {
		matched := -1
		for index, tool := range tools {
			if tool.Type != llm.ToolTypeFunction {
				continue
			}
			matches := tool.Function.Name == name
			if !exact {
				matches = tool.Function.Namespace != "" &&
					strings.TrimPrefix(tool.Function.Name, tool.Function.Namespace+"__") == name
			}
			if matches {
				if matched >= 0 {
					return -1
				}
				matched = index
			}
		}
		if matched >= 0 {
			return matched
		}
	}
	return -1
}

func validateAllowedToolsHistory(req *llm.Request) error {
	if req.ProviderExtensions != nil && req.ProviderExtensions.OpenAIResponses != nil {
		ext := req.ProviderExtensions.OpenAIResponses.Request
		if ext != nil && len(ext.RawInputItems) > 0 {
			return fmt.Errorf("%w: allowed_tools conversion cannot preserve native Responses input items", transformer.ErrInvalidRequest)
		}
	}
	for _, message := range req.Messages {
		if len(message.InlineToolResults) > 0 {
			return fmt.Errorf("%w: allowed_tools conversion cannot preserve server tool results", transformer.ErrInvalidRequest)
		}
		for _, call := range message.ToolCalls {
			if call.ResponseCustomToolCall != nil || (call.Type != "" && call.Type != llm.ToolTypeFunction) {
				return fmt.Errorf("%w: allowed_tools conversion cannot preserve non-function tool history", transformer.ErrInvalidRequest)
			}
		}
		for _, part := range message.Content.MultipleContent {
			if part.Compact != nil || part.Type == "compaction" || part.Type == "compaction_summary" {
				return fmt.Errorf("%w: allowed_tools conversion cannot preserve compaction history", transformer.ErrInvalidRequest)
			}
		}
	}
	return nil
}
