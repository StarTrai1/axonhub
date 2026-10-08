package responses

import (
	"strings"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/llm"
)

func (s *responsesOutboundStream) refusalPart(event StreamEvent) *outboundTextState {
	key := textPartKey(event)
	part := s.state.refusalParts[key]
	if part == nil {
		part = &outboundTextState{phase:s.state.currentMessagePhase}
		s.state.refusalParts[key] = part
	}
	return part
}

func (s *responsesOutboundStream) refusalDelta(event StreamEvent, text string) *llm.Message {
	part := s.refusalPart(event)
	part.content.WriteString(text)
	s.state.refusalDelivered = s.state.refusalDelivered || text != ""
	return &llm.Message{Phase:part.phase, Refusal:text}
}

func (s *responsesOutboundStream) recoverRefusalDone(event StreamEvent, text string) *llm.Message {
	part, known := s.state.refusalParts[textPartKey(event)]
	if !known && s.state.refusalDelivered { return nil }
	forwarded := ""
	if known { forwarded = part.content.String() }
	if len(text) <= len(forwarded) || !strings.HasPrefix(text, forwarded) { return nil }
	return s.refusalDelta(event, text[len(forwarded):])
}

func (s *responsesOutboundStream) recoverItemRefusal(base *llm.Response, event StreamEvent) {
	if event.Item == nil || event.Item.Content == nil { return }
	for index, content := range event.Item.Content.Items {
		if content.Type != "refusal" { continue }
		partEvent := event
		partEvent.ItemID = lo.ToPtr(event.Item.ID)
		partEvent.ContentIndex = lo.ToPtr(index)
		if delta := s.recoverRefusalDone(partEvent, lo.FromPtr(content.Refusal)); delta != nil {
			delta.Phase = event.Item.Phase
			recovered := *base
			recovered.Choices = []llm.Choice{{Index:0, Delta:delta}}
			s.enqueue(&recovered)
		}
	}
}

// Terminal snapshots are a fallback only before any refusal has been emitted;
// their item/index association may differ from the streamed content.
func (s *responsesOutboundStream) recoverTerminalRefusal(base *llm.Response, response *Response) {
	if s.state.refusalDelivered || response == nil || response.Error != nil { return }
	switch lo.FromPtr(response.Status) {
	case "failed", "cancelled", "canceled": return
	}
	for _, item := range response.Output {
		if item.Type != "message" || (item.Role != "" && item.Role != "assistant") || item.Content == nil { continue }
		for _, content := range item.Content.Items {
			if content.Type != "refusal" || lo.FromPtr(content.Refusal) == "" { continue }
			recovered := *base
			recovered.Choices = []llm.Choice{{Index:0, Delta:&llm.Message{Phase:item.Phase, Refusal:*content.Refusal}}}
			s.state.refusalDelivered = true
			s.enqueue(&recovered)
		}
	}
}
