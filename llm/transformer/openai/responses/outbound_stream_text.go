package responses

import (
	"strings"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/llm"
)

type outboundTextPart struct {
	itemID       string
	outputIndex  int
	contentIndex int
}

type outboundTextState struct {
	content strings.Builder
	phase   *string
}

func textPartKey(event StreamEvent) outboundTextPart {
	return outboundTextPart{
		itemID:       lo.FromPtr(event.ItemID),
		outputIndex:  event.OutputIndex,
		contentIndex: lo.FromPtr(event.ContentIndex),
	}
}

func (s *responsesOutboundStream) textPart(event StreamEvent) *outboundTextState {
	key := textPartKey(event)
	part := s.state.textParts[key]
	if part == nil {
		part = &outboundTextState{phase: s.state.currentMessagePhase}
		s.state.textParts[key] = part
	}
	return part
}

func (s *responsesOutboundStream) textDelta(event StreamEvent, text string) *llm.Message {
	part := s.textPart(event)
	part.content.WriteString(text)
	s.state.textDelivered = s.state.textDelivered || text != ""
	return &llm.Message{
		Phase:   part.phase,
		Content: llm.MessageContent{Content: lo.ToPtr(text)},
	}
}

// A done event carries the whole part, not another delta. Only append a tail
// when it extends exactly what was delivered for the same item and indices.
func (s *responsesOutboundStream) recoverTextDone(event StreamEvent) *llm.Message {
	part, known := s.state.textParts[textPartKey(event)]
	if !known && s.state.textDelivered {
		return nil
	}
	forwarded := ""
	if known {
		forwarded = part.content.String()
	}
	if len(event.Text) <= len(forwarded) || !strings.HasPrefix(event.Text, forwarded) {
		return nil
	}
	return s.textDelta(event, event.Text[len(forwarded):])
}

// Terminal output arrays are a fallback only when no assistant text was sent.
// Their indices need not match streamed events, so do not merge them with an
// already visible answer. Explicit failures retain their existing error path.
func (s *responsesOutboundStream) recoverTerminalText(base *llm.Response, response *Response) {
	if s.state.textDelivered || response == nil || response.Error != nil {
		return
	}
	switch lo.FromPtr(response.Status) {
	case "failed", "cancelled", "canceled":
		return
	}
	for _, item := range response.Output {
		if item.Type != "message" || (item.Role != "" && item.Role != "assistant") || item.Content == nil {
			continue
		}
		for _, content := range item.Content.Items {
			if content.Type != "output_text" || lo.FromPtr(content.Text) == "" {
				continue
			}
			recovered := *base
			recovered.Choices = []llm.Choice{{Index: 0, Delta: &llm.Message{
				Phase:   item.Phase,
				Content: llm.MessageContent{Content: content.Text},
			}}}
			s.state.textDelivered = true
			s.enqueue(&recovered)
		}
	}
}
