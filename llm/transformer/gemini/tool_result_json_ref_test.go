package gemini

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
)

func TestToolResultJSONReferencesRemainOpaque(t *testing.T) {
	for _, raw := range []string{
		`{"$ref":"#/$defs/Item","number":9007199254740993}`,
		`{"schema":{"properties":{"items":[{"$ref":"#/components/Item"}]}}}`,
		`[{"$ref":"#/Item"}]`,
	} {
		t.Run(raw, func(t *testing.T) {
			msg := &llm.Message{
				Role: "tool", ToolCallID: lo.ToPtr("call-1"),
				Content: llm.MessageContent{Content: lo.ToPtr(raw)},
			}
			previous := []*Content{{Role: "model", Parts: []*Part{{
				FunctionCall: &FunctionCall{ID: "call-1", Name: "schema_lookup"},
			}}}}
			result := convertLLMToolResultToGeminiContent(msg, previous).Parts[0].FunctionResponse
			require.Equal(t, "call-1", result.ID)
			require.Equal(t, "schema_lookup", result.Name)
			require.Equal(t, map[string]any{"result": raw}, result.Response)
			require.Equal(t, raw, *msg.Content.Content)
		})
	}
}

func TestOrdinaryToolResultObjectsKeepTheirStructure(t *testing.T) {
	for _, raw := range []string{`{"value":42}`, `{"$ref":23}`, `{"text":"the word $ref is not a reference"}`} {
		var expected map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &expected))
		msg := &llm.Message{Content: llm.MessageContent{Content: lo.ToPtr(raw)}}
		result := convertLLMToolResultToGeminiContent(msg, nil).Parts[0].FunctionResponse
		require.Equal(t, expected, result.Response)
	}
}
