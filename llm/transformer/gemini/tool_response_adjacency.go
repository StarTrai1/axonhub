package gemini

// Gemini requires one function-response turn immediately after its call turn.
// Anthropic can interleave reminders with parallel results in consecutive user
// messages. Move only a complete, uniquely matched set ahead of those reminders;
// do not guess missing results or cross another model turn. Native pass-through
// and the shared input history are not changed by this outbound conversion.
func adjacentGeminiToolResponses(contents []*Content) []*Content {
	result := make([]*Content, 0, len(contents))
	for i := 0; i < len(contents); i++ {
		result = append(result, contents[i])
		end, responses, trailing := groupedGeminiToolResponses(contents, i)
		if end <= i+1 {
			continue
		}
		result = append(result, &Content{Role: "user", Parts: responses})
		result = append(result, trailing...)
		i = end
	}
	return result
}

func groupedGeminiToolResponses(contents []*Content, index int) (int, []*Part, []*Content) {
	call := contents[index]
	if call == nil || call.Role != "model" {
		return index, nil, nil
	}
	pending := make(map[string]bool)
	for _, part := range call.Parts {
		if part == nil || part.FunctionCall == nil {
			continue
		}
		id := part.FunctionCall.ID
		if id == "" || pending[id] {
			return index, nil, nil
		}
		pending[id] = true
	}
	if len(pending) == 0 {
		return index, nil, nil
	}
	var responses []*Part
	var trailing []*Content
	for end := index + 1; end < len(contents); end++ {
		content := contents[end]
		if content == nil || content.Role != "user" {
			break
		}
		var rest []*Part
		for _, part := range content.Parts {
			if part == nil || part.FunctionResponse == nil {
				rest = append(rest, part)
				continue
			}
			id := part.FunctionResponse.ID
			if !pending[id] {
				return index, nil, nil
			}
			delete(pending, id)
			responses = append(responses, part)
		}
		if len(rest) > 0 {
			copy := *content
			copy.Parts = rest
			trailing = append(trailing, &copy)
		}
		if len(pending) == 0 {
			return end, responses, trailing
		}
	}
	return index, nil, nil
}
