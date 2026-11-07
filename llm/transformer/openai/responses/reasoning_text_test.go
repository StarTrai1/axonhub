package responses

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestReasoningContentFallbackAcrossConversions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		summary []ReasoningSummary
		want    string
	}{
		{name: "content only", want: "first second"},
		{name: "empty summary", summary: []ReasoningSummary{{Type: "summary_text"}}, want: "first second"},
		{name: "summary takes precedence", summary: []ReasoningSummary{{Type: "summary_text", Text: "summary"}}, want: "summary"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := Item{Type: "reasoning", ID: "rs_original", Summary: tc.summary, EncryptedContent: lo.ToPtr("opaque"), Content: &Input{Items: []Item{
				{Type: "reasoning_text", Text: lo.ToPtr("first ")},
				{Type: "output_text", Text: lo.ToPtr("not reasoning")},
				{Type: "reasoning_text", Text: lo.ToPtr("second")},
			}}}
			inbound, consumed, err := convertReasoningWithFollowing([]Item{item}, 0)
			require.NoError(t, err)
			require.Equal(t, 1, consumed)
			require.Equal(t, tc.want, lo.FromPtr(inbound.ReasoningContent))
			require.Equal(t, "rs_original", inbound.ReasoningItems[0].ID)
			require.Equal(t, "opaque", inbound.ReasoningItems[0].Signature)
			outbound := convertOutputToMessage([]Item{item}, nil)
			require.Equal(t, tc.want, lo.FromPtr(outbound.ReasoningContent))
			require.Equal(t, "rs_original", outbound.ReasoningItems[0].ID)
			require.Equal(t, tc.want, outbound.ReasoningItems[0].Content)
			require.NotEmpty(t, outbound.ReasoningItems[0].Signature)
		})
	}
}
