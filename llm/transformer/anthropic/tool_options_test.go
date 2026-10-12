package anthropic

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
)

func TestFunctionToolPreservesDeferredLoadingOptions(t *testing.T) {
	deferred := true
	strict := true
	requestTool := Tool{
		Name:           "lookup",
		Description:    "Look up a record",
		InputSchema:    json.RawMessage(`{"type":"object","properties":{}}`),
		Strict:         &strict,
		AllowedCallers: []string{"direct", "code_execution_20260120"},
		DeferLoading:   &deferred,
		InputExamples: []json.RawMessage{
			json.RawMessage(`{"query":"example"}`),
		},
	}

	unified, ok := convertToolToLLM(requestTool)
	require.True(t, ok)
	require.Equal(t, requestTool.AllowedCallers, unified.AllowedCallers)
	require.Equal(t, requestTool.DeferLoading, unified.DeferLoading)
	require.Equal(t, requestTool.InputExamples, unified.InputExamples)
	require.Equal(t, requestTool.Strict, unified.Function.Strict)

	roundTripped := convertToolsAnthropic([]llm.Tool{unified}, nil)
	require.Len(t, roundTripped, 1)
	require.Equal(t, requestTool.AllowedCallers, roundTripped[0].AllowedCallers)
	require.Equal(t, requestTool.DeferLoading, roundTripped[0].DeferLoading)
	require.Equal(t, requestTool.InputExamples, roundTripped[0].InputExamples)
	require.Equal(t, requestTool.Strict, roundTripped[0].Strict)
}

func TestDeferredToolsNeverCarryPromptCacheBreakpoints(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		for _, eager := range []bool{false, true} {
			t.Run(fmt.Sprintf("automatic=%v/eager=%v", automatic, eager), func(t *testing.T) {
				request := &llm.Request{
					Model: "claude-sonnet-4-6", MaxTokens: lo.ToPtr(int64(128)),
					Messages: []llm.Message{{Role: "user", Content: llm.MessageContent{Content: lo.ToPtr("hello")}, CacheControl: &llm.CacheControl{Type: "ephemeral"}}},
				}
				if eager {
					request.Tools = append(request.Tools, llm.Tool{Type: "function", Function: llm.Function{Name: "eager", Parameters: json.RawMessage(`{"type":"object"}`)}, DeferLoading: lo.ToPtr(false)})
				}
				request.Tools = append(request.Tools, llm.Tool{Type: "function", Function: llm.Function{Name: "deferred", Parameters: json.RawMessage(`{"type":"object"}`)}, DeferLoading: lo.ToPtr(true), CacheControl: &llm.CacheControl{Type: "ephemeral", TTL: "1h"}})
				if automatic {
					request.TransformerMetadata = map[string]any{TransformerMetadataKeyCacheControl: &CacheControl{Type: "ephemeral"}}
				}
				before, err := json.Marshal(request)
				require.NoError(t, err)
				outbound, err := NewOutboundTransformer("https://example.com", "test-key")
				require.NoError(t, err)
				wire, err := outbound.TransformRequest(t.Context(), request)
				require.NoError(t, err)
				var sent MessageRequest
				require.NoError(t, json.Unmarshal(wire.Body, &sent))
				last := sent.Tools[len(sent.Tools)-1]
				require.True(t, *last.DeferLoading)
				require.Nil(t, last.CacheControl)
				if eager && !automatic {
					require.NotNil(t, sent.Tools[0].CacheControl)
				}
				if automatic {
					require.NotNil(t, sent.CacheControl)
				}
				after, err := json.Marshal(request)
				require.NoError(t, err)
				require.JSONEq(t, string(before), string(after))
			})
		}
	}
}
