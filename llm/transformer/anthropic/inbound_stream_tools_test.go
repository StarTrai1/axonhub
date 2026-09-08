package anthropic

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/streams"
)

func interleavedToolDelta(index int, identifier, name, arguments string) llm.ToolCall {
	return llm.ToolCall{
		Index: index,
		ID:    identifier,
		Type:  "function",
		Function: llm.FunctionCall{
			Name:      name,
			Arguments: arguments,
		},
	}
}

func interleavedToolChunk(calls ...llm.ToolCall) *llm.Response {
	return &llm.Response{
		ID:      "response_interleaved_astra",
		Model:   "gpt-6-astra",
		Choices: []llm.Choice{{Delta: &llm.Message{ToolCalls: calls}}},
	}
}

func collectAnthropicToolEvents(test *testing.T, source streams.Stream[*llm.Response]) ([]StreamEvent, error) {
	test.Helper()
	stream, err := NewInboundTransformer().TransformStream(test.Context(), source)
	require.NoError(test, err)
	defer stream.Close()
	var events []StreamEvent
	for stream.Next() {
		var event StreamEvent
		require.NoError(test, json.Unmarshal(stream.Current().Data, &event))
		events = append(events, event)
	}
	return events, stream.Err()
}

func requireSequentialTools(test *testing.T, events []StreamEvent, identifiers []string) map[string]string {
	test.Helper()
	activeIndex := int64(-1)
	activeID := ""
	arguments := make(map[string]string)
	var actualIDs []string
	for _, event := range events {
		switch event.Type {
		case "content_block_start":
			require.Equal(test, int64(-1), activeIndex, "content blocks must not overlap")
			require.NotNil(test, event.Index)
			activeIndex = *event.Index
			activeID = event.ContentBlock.ID
			if event.ContentBlock.Type == "tool_use" {
				actualIDs = append(actualIDs, activeID)
				arguments[activeID] = ""
			}
		case "content_block_delta":
			require.NotNil(test, event.Index)
			require.Equal(test, activeIndex, *event.Index, "delta must target the open block")
			if event.Delta.PartialJSON != nil {
				arguments[activeID] += *event.Delta.PartialJSON
			}
		case "content_block_stop":
			require.NotNil(test, event.Index)
			require.Equal(test, activeIndex, *event.Index)
			activeIndex = -1
			activeID = ""
		}
	}
	require.Equal(test, int64(-1), activeIndex)
	require.Equal(test, identifiers, actualIDs)
	require.Equal(test, 1, countStreamEvents(events, "message_stop"))
	return arguments
}

func TestInboundStreamSerializesInterleavedToolArguments(test *testing.T) {
	test.Parallel()
	for _, scenario := range []struct {
		name   string
		chunks []*llm.Response
	}{
		{
			name: "parallel starts in one chunk",
			chunks: []*llm.Response{
				interleavedToolChunk(interleavedToolDelta(0, "call_first", "first", `{"value":`), interleavedToolDelta(1, "call_second", "second", `{"value":`)),
				interleavedToolChunk(interleavedToolDelta(1, "", "", `2}`)),
				interleavedToolChunk(interleavedToolDelta(0, "", "", `1}`)),
			},
		},
		{
			name: "non-monotonic indexes and content between fragments",
			chunks: []*llm.Response{
				interleavedToolChunk(interleavedToolDelta(9, "call_first", "first", `{"value":`)),
				interleavedToolChunk(interleavedToolDelta(0, "call_second", "second", `{"value":`)),
				{Choices: []llm.Choice{{Delta: &llm.Message{Content: llm.MessageContent{Content: lo.ToPtr("progress")}}}}},
				{Choices: []llm.Choice{{Delta: &llm.Message{ReasoningContent: lo.ToPtr("explanation"), ReasoningSignature: lo.ToPtr("real-signature")}}}},
				interleavedToolChunk(interleavedToolDelta(0, "", "", `2}`)),
				interleavedToolChunk(interleavedToolDelta(9, "", "", `1}`)),
			},
		},
		{
			name: "late tool identity",
			chunks: []*llm.Response{
				interleavedToolChunk(interleavedToolDelta(0, "call_first", "first", `{"value":`)),
				interleavedToolChunk(interleavedToolDelta(1, "", "", `{"value":`)),
				interleavedToolChunk(interleavedToolDelta(0, "", "", `1}`)),
				interleavedToolChunk(interleavedToolDelta(1, "call_second", "second", `2}`)),
			},
		},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			test.Parallel()
			scenario.chunks = append(scenario.chunks,
				&llm.Response{Choices: []llm.Choice{{FinishReason: lo.ToPtr("tool_calls")}}},
				&llm.Response{Usage: &llm.Usage{PromptTokens: 10, CompletionTokens: 8, TotalTokens: 18}},
			)
			original, err := json.Marshal(scenario.chunks)
			require.NoError(test, err)
			events, err := collectAnthropicToolEvents(test, streams.SliceStream(scenario.chunks))
			require.NoError(test, err)
			arguments := requireSequentialTools(test, events, []string{"call_first", "call_second"})
			require.JSONEq(test, `{"value":1}`, arguments["call_first"])
			require.JSONEq(test, `{"value":2}`, arguments["call_second"])
			for _, event := range events {
				if event.Type == "message_delta" {
					require.Equal(test, int64(8), event.Usage.OutputTokens)
				}
			}
			after, err := json.Marshal(scenario.chunks)
			require.NoError(test, err)
			require.Equal(test, string(original), string(after), "source history must remain untouched")
			if scenario.name == "non-monotonic indexes and content between fragments" {
				encoded, err := json.Marshal(events)
				require.NoError(test, err)
				require.Contains(test, string(encoded), "progress")
				require.Contains(test, string(encoded), "explanation")
				require.Contains(test, string(encoded), "real-signature")
			}
		})
	}
}

func TestInboundStreamInterleavedEmptyArgumentsAndCleanEOF(test *testing.T) {
	test.Parallel()
	events, err := collectAnthropicToolEvents(test, streams.SliceStream([]*llm.Response{
		interleavedToolChunk(interleavedToolDelta(0, "call_empty", "empty", "")),
		interleavedToolChunk(interleavedToolDelta(1, "call_second", "second", `{"value":2}`)),
	}))
	require.NoError(test, err)
	arguments := requireSequentialTools(test, events, []string{"call_empty", "call_second"})
	require.Empty(test, arguments["call_empty"])
	require.JSONEq(test, `{"value":2}`, arguments["call_second"])
}

func TestInboundStreamInterleavedToolsFailClosed(test *testing.T) {
	test.Parallel()
	transportFailure := errors.New("upstream disconnected")
	for _, scenario := range []struct {
		name   string
		source streams.Stream[*llm.Response]
		error  string
	}{
		{
			name: "invalid arguments",
			source: streams.SliceStream([]*llm.Response{
				interleavedToolChunk(interleavedToolDelta(0, "call_first", "first", `{"value":`)),
				interleavedToolChunk(interleavedToolDelta(1, "call_second", "second", `{}`)),
			}),
			error: "invalid tool call arguments",
		},
		{
			name: "transport failure",
			source: &failingResponseStream{
				items: []*llm.Response{
					interleavedToolChunk(interleavedToolDelta(0, "call_first", "first", `{"value":`)),
					interleavedToolChunk(interleavedToolDelta(1, "call_second", "second", `{}`)),
				},
				err: transportFailure,
			},
			error: transportFailure.Error(),
		},
	} {
		test.Run(scenario.name, func(test *testing.T) {
			test.Parallel()
			events, err := collectAnthropicToolEvents(test, scenario.source)
			require.ErrorContains(test, err, scenario.error)
			require.Zero(test, countStreamEvents(events, "message_stop"))
			require.Zero(test, countStreamEvents(events, "message_delta"))
		})
	}
}

func TestInterleavedToolBufferIsBounded(test *testing.T) {
	test.Parallel()
	buffer := interleavedToolBuffer{calls: make(map[int]*llm.ToolCall)}
	oversized := interleavedToolChunk(interleavedToolDelta(0, "call_large", "large", strings.Repeat(" ", maxInterleavedToolBytes)))
	require.ErrorContains(test, buffer.appendChunk(oversized), "buffer limit")
	require.Empty(test, buffer.chunks)
	for range maxInterleavedToolChunks {
		require.NoError(test, buffer.appendChunk(&llm.Response{}))
	}
	require.ErrorContains(test, buffer.appendChunk(&llm.Response{}), "buffer limit")
}

type countedToolResponseStream struct {
	streams.Stream[*llm.Response]
	reads int
}

func (stream *countedToolResponseStream) Next() bool {
	stream.reads++
	return stream.Stream.Next()
}

func TestInboundStreamKeepsFirstToolStreaming(test *testing.T) {
	test.Parallel()
	source := &countedToolResponseStream{Stream: streams.SliceStream([]*llm.Response{
		interleavedToolChunk(interleavedToolDelta(0, "call_first", "first", `{"value":`)),
		interleavedToolChunk(interleavedToolDelta(0, "", "", `1}`)),
		interleavedToolChunk(interleavedToolDelta(1, "call_second", "second", `{}`)),
	})}
	stream, err := NewInboundTransformer().TransformStream(test.Context(), source)
	require.NoError(test, err)
	defer stream.Close()
	require.True(test, stream.Next())
	require.Equal(test, 1, source.reads, "the ordinary path must not pre-read the tool stream")
}
