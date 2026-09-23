package openai

import (
	"strings"

	"github.com/samber/lo"
)

// relayToolResultMedia moves media from converted tool results to a user
// message, as Chat Completions tool messages only accept text. Keep a contiguous
// group of tool replies together before emitting their media.
func relayToolResultMedia(messages []Message) []Message {
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
		var remaining, media []MessageContentPart
		for _, part := range message.Content.MultipleContent {
			switch {
			case part.Type == "image_url" && part.ImageURL != nil,
				part.Type == "file" && part.File != nil,
				part.Type == "input_audio" && part.InputAudio != nil,
				part.Type == "video_url" && part.VideoURL != nil:
				media = append(media, part)
			default:
				remaining = append(remaining, part)
			}
		}
		if len(media) > 0 {
			changed = true
			message.Content = MessageContent{MultipleContent: remaining}
			if len(remaining) == 0 {
				message.Content.Content = lo.ToPtr("[Tool returned media; see the following user message.]")
			}
			label := "Media returned by the preceding tool call:"
			if message.ToolCallID != nil && *message.ToolCallID != "" {
				label = "Media returned by tool call " + *message.ToolCallID + ":"
			}
			pending = append(pending, MessageContentPart{Type: "text", Text: &label})
			pending = append(pending, media...)
		}
		if len(message.Content.MultipleContent) > 0 {
			texts := make([]string, 0, len(message.Content.MultipleContent))
			textOnly := true
			for _, part := range message.Content.MultipleContent {
				if part.Type != "text" || part.Text == nil {
					textOnly = false
					break
				}
				texts = append(texts, *part.Text)
			}
			if textOnly {
				message.Content = MessageContent{Content: lo.ToPtr(strings.Join(texts, "\n\n"))}
				changed = true
			}
		}
		result = append(result, message)
	}
	flush()
	if !changed {
		return messages
	}
	return result
}
