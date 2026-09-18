package antigravity

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/gemini"
)

func TestStripClaudeAttribution(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{
		{"x-anthropic-billing-header: cc_version=synthetic", ""},
		{" \r\nx-anthropic-billing-header: synthetic\nkeep instructions", "keep instructions"},
		{"x-anthropic-billing-header: synthetic\r\n keep spacing\n", " keep spacing\n"},
		{"x-anthropic-billing-header: synthetic\rkeep instructions", "keep instructions"},
		{"x-anthropic-billing-header without colon", "x-anthropic-billing-header without colon"},
		{"quoted example\nx-anthropic-billing-header: synthetic", "quoted example\nx-anthropic-billing-header: synthetic"},
	} {
		require.Equal(t, test.want, stripClaudeAttribution(test.input))
	}
}

func TestAntigravityAttributionIsScopedToSystemParts(t *testing.T) {
	const metadata = "x-anthropic-billing-header: cc_version=synthetic"
	request := &llm.Request{
		Model: "gemini-3-pro",
		Messages: []llm.Message{
			{Role: "system", Content: llm.MessageContent{MultipleContent: []llm.MessageContentPart{
				{Type: "text", Text: lo.ToPtr(metadata + "\nkeep instructions")},
				{Type: "text", Text: lo.ToPtr(metadata)},
				{Type: "text", Text: lo.ToPtr("example\n" + metadata)},
			}}},
			{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr(metadata)}},
		},
	}
	before, err := json.Marshal(request)
	require.NoError(t, err)
	native, err := gemini.NewOutboundTransformer("https://example.com", "synthetic-key")
	require.NoError(t, err)
	wire, err := native.TransformRequest(t.Context(), request)
	require.NoError(t, err)
	var converted gemini.GenerateContentRequest
	require.NoError(t, json.Unmarshal(wire.Body, &converted))
	require.Equal(t, metadata, converted.SystemInstruction.Parts[1].Text)

	adapter := &Transformer{}
	require.NoError(t, adapter.patchGeminiRequest(t.Context(), &converted, request))
	require.Contains(t, converted.SystemInstruction.Parts[0].Text, "keep instructions")
	require.NotContains(t, converted.SystemInstruction.Parts[0].Text, metadata)
	require.Len(t, converted.SystemInstruction.Parts, 2)
	require.Equal(t, "example\n"+metadata, converted.SystemInstruction.Parts[1].Text)
	require.Equal(t, metadata, converted.Contents[0].Parts[0].Text)
	after, err := json.Marshal(request)
	require.NoError(t, err)
	require.Equal(t, before, after)
	// The native Gemini request retains the metadata; only the copied envelope changes.
	require.Contains(t, string(wire.Body), metadata)
}
