package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/streams"
)

func TestThinkingUsagePreservesInclusiveTotals(test *testing.T) {
	test.Parallel()
	var native Usage
	require.NoError(test, json.Unmarshal([]byte(`{"input_tokens":100,"output_tokens":80,"output_tokens_details":{"thinking_tokens":60},"cache_read_input_tokens":30,"cache_creation_input_tokens":20}`), &native))
	unified := convertToLlmUsage(&native, PlatformDirect)
	require.Equal(test, int64(60), unified.CompletionTokensDetails.ReasoningTokens)
	require.Equal(test, int64(150), unified.PromptTokens)
	require.Equal(test, int64(80), unified.CompletionTokens)
	require.Equal(test, int64(230), unified.TotalTokens)

	restored := convertToAnthropicUsage(unified)
	require.Equal(test, native.OutputTokensDetails, restored.OutputTokensDetails)
	require.Equal(test, native.InputTokens, restored.InputTokens)
	require.Equal(test, native.OutputTokens, restored.OutputTokens)
	require.Equal(test, native.CacheReadInputTokens, restored.CacheReadInputTokens)
	require.Equal(test, native.CacheCreationInputTokens, restored.CacheCreationInputTokens)
}

func TestThinkingUsageBounds(test *testing.T) {
	test.Parallel()
	for _, scenario := range []struct {
		name     string
		thinking int64
		output   int64
		expected int64
	}{
		{name: "positive", thinking: 60, output: 80, expected: 60},
		{name: "bounded by output", thinking: 90, output: 80, expected: 80},
		{name: "large token counts", thinking: 1 << 40, output: 1 << 41, expected: 1 << 40},
		{name: "negative thinking", thinking: -1, output: 80},
		{name: "zero thinking", output: 80},
		{name: "zero output", thinking: 60},
		{name: "negative output", thinking: 60, output: -1},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			test.Parallel()
			native := &Usage{
				OutputTokens:        scenario.output,
				OutputTokensDetails: &OutputTokensDetails{ThinkingTokens: scenario.thinking},
			}
			unified := convertToLlmUsage(native, PlatformDirect)
			require.Equal(test, scenario.expected, unified.CompletionTokensDetails.ReasoningTokens)
			require.Equal(test, scenario.output, unified.CompletionTokens)

			converted := convertToAnthropicUsage(&llm.Usage{
				CompletionTokens:        scenario.output,
				CompletionTokensDetails: &llm.CompletionTokensDetails{ReasoningTokens: scenario.thinking},
			})
			if scenario.expected == 0 {
				require.Nil(test, converted.OutputTokensDetails)
			} else {
				require.Equal(test, scenario.expected, converted.OutputTokensDetails.ThinkingTokens)
			}
			require.Equal(test, scenario.output, converted.OutputTokens)
		})
	}
}

func TestInboundStreamReportsThinkingUsageAtTerminal(test *testing.T) {
	test.Parallel()
	stream, err := NewInboundTransformer().TransformStream(test.Context(), streams.SliceStream([]*llm.Response{
		{
			ID:    "resp_astra_usage",
			Model: "gpt-6-astra",
			Choices: []llm.Choice{{
				Delta: &llm.Message{Role: "assistant", Content: llm.MessageContent{Content: lo.ToPtr("done")}},
			}},
		},
		{
			ID:    "resp_astra_usage",
			Model: "gpt-6-astra",
			Usage: &llm.Usage{
				PromptTokens:            100,
				CompletionTokens:        80,
				CompletionTokensDetails: &llm.CompletionTokensDetails{ReasoningTokens: 60},
				TotalTokens:             180,
			},
			Choices: []llm.Choice{{FinishReason: lo.ToPtr("stop")}},
		},
	}))
	require.NoError(test, err)
	defer stream.Close()
	var terminal *Usage
	for stream.Next() {
		var event StreamEvent
		require.NoError(test, json.Unmarshal(stream.Current().Data, &event))
		if event.Type == "message_delta" {
			terminal = event.Usage
		}
	}
	require.NoError(test, stream.Err())
	require.NotNil(test, terminal)
	require.Equal(test, int64(80), terminal.OutputTokens)
	require.Equal(test, &OutputTokensDetails{ThinkingTokens: 60}, terminal.OutputTokensDetails)
}
