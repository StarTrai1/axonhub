package openai

import "github.com/samber/lo"

// relayToolResultImages moves images from converted tool results to a user
// message, as Chat Completions tool messages only accept text. Keep a contiguous
// group of tool replies together before emitting their images.
func relayToolResultImages(messages []Message) []Message {
	result := make([]Message, 0, len(messages))
	var pending []MessageContentPart
	changed := false
	flush := func() {
		if len(pending) > 0 {
			result = append(result, Message{Role: "user", Content: MessageContent{MultipleContent: pending}})
			pending = nil
		}
	}
	for _, message := range messages {
		if message.Role != "tool" {
			flush()
			result = append(result, message)
			continue
		}
		var remaining, images []MessageContentPart
		for _, part := range message.Content.MultipleContent {
			if part.Type == "image_url" && part.ImageURL != nil {
				images = append(images, part)
			} else {
				remaining = append(remaining, part)
			}
		}
		if len(images) > 0 {
			changed = true
			message.Content = MessageContent{MultipleContent: remaining}
			if len(remaining) == 0 {
				message.Content.Content = lo.ToPtr("[Tool returned images; see the following user message.]")
			}
			label := "Images returned by the preceding tool call:"
			if message.ToolCallID != nil && *message.ToolCallID != "" {
				label = "Images returned by tool call " + *message.ToolCallID + ":"
			}
			pending = append(pending, MessageContentPart{Type: "text", Text: &label})
			pending = append(pending, images...)
		}
		result = append(result, message)
	}
	flush()
	if !changed {
		return messages
	}
	return result
}
