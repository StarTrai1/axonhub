package antigravity

import "github.com/looplj/axonhub/llm/transformer/gemini"

func thoughtOnlyModelTurn(content *gemini.Content) bool {
	if content == nil || content.Role != "model" || len(content.Parts) == 0 {
		return false
	}
	for _, part := range content.Parts {
		if part == nil || !part.Thought || part.FunctionCall != nil || part.FunctionResponse != nil ||
			part.InlineData != nil || part.FileData != nil {
			return false
		}
	}
	return true
}
