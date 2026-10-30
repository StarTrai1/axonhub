package gemini

// Keep a function-response turn separate from its trailing media. Vertex can
// reject a mixed turn as ending on the model; splitting every converted turn
// also keeps the history prefix stable when another user message is appended.
// Gemini 3's native functionResponse.parts remain inside the response itself.
func splitGeminiToolResponseMedia(contents []*Content) []*Content {
	var result []*Content
	for index, content := range contents {
		responseCount := 0
		if content != nil && content.Role == "user" {
			for _, part := range content.Parts {
				if part != nil && part.FunctionResponse != nil {
					responseCount++
				}
			}
		}
		if responseCount == 0 || responseCount == len(content.Parts) {
			if result != nil {
				result = append(result, content)
			}
			continue
		}
		if result == nil {
			result = append(make([]*Content, 0, len(contents)+1), contents[:index]...)
		}
		responses := make([]*Part, 0, responseCount)
		rest := make([]*Part, 0, len(content.Parts)-responseCount)
		for _, part := range content.Parts {
			if part != nil && part.FunctionResponse != nil {
				responses = append(responses, part)
			} else {
				rest = append(rest, part)
			}
		}
		calls, trailing := *content, *content
		calls.Parts, trailing.Parts = responses, rest
		result = append(result, &calls, &trailing)
	}
	if result == nil {
		return contents
	}
	return result
}
