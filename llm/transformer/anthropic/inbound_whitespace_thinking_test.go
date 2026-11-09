package anthropic

import (
	"strings"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
)

func TestResponseWhitespaceThinking(t *testing.T) {
	for _, tc := range []struct {
		name, text, signature string
		wantThinking bool
	}{
		{"blank", " \n\t", "", false},
		{"text", " think ", "", true},
		{"signed blank", " \n", "opaque-signature", true},
		{"signature only", "", "opaque-signature", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := convertToAnthropicResponse(&llm.Response{Choices: []llm.Choice{{Message: &llm.Message{
				Role: "assistant", ReasoningContent: lo.ToPtr(tc.text), ReasoningSignature: lo.EmptyableToPtr(tc.signature),
				Content: llm.MessageContent{Content: lo.ToPtr("answer")},
			}}}})
			var thinking []MessageContentBlock
			for _, block := range response.Content {
				if block.Type == "thinking" { thinking = append(thinking, block) }
			}
			if !tc.wantThinking {
				require.Empty(t, thinking)
				return
			}
			require.Len(t, thinking, 1)
			require.Equal(t, tc.text, lo.FromPtr(thinking[0].Thinking))
			if tc.signature != "" { require.Equal(t, tc.signature, lo.FromPtr(thinking[0].Signature)) }
		})
	}
}

func TestStreamWhitespaceDoesNotOpenThinking(t *testing.T) {
	for _, itemID := range []string{"", "rs_reasoning"} {
		for _, tc := range []struct {
			name string
			deltas []string
			wantBlocks int
			wantText string
		}{
			{"only whitespace", []string{" ", "\t\n"}, 0, ""},
			{"spaces within thinking", []string{"think", " ", "more", "\n"}, 1, "think more\n"},
		} {
			t.Run(itemID+"/"+tc.name, func(t *testing.T) {
				var chunks []*llm.Response
				for _, delta := range tc.deltas {
					opt := withReasoningContent(delta)
					if itemID != "" { opt = withReasoningContentForItem(delta, itemID) }
					chunks = append(chunks, buildChunk("response", "model", opt))
				}
				chunks = append(chunks, buildChunk("response", "model", withTextContent("answer")), buildChunk("response", "model", withFinishReason("stop")))
				events := collectStreamEvents(t, chunks)
				blocks := 0
				var thinking strings.Builder
				for _, event := range events {
					if event.ContentBlock != nil && event.ContentBlock.Type == "thinking" { blocks++ }
					if event.Delta != nil && lo.FromPtr(event.Delta.Type) == "thinking_delta" { thinking.WriteString(lo.FromPtr(event.Delta.Thinking)) }
				}
				require.Equal(t, tc.wantBlocks, blocks)
				require.Equal(t, tc.wantText, thinking.String())
			})
		}
	}
}
