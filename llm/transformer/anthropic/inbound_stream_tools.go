package anthropic

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/streams"
)

const (
	maxInterleavedToolChunks = 4096
	maxInterleavedToolBytes  = 8 << 20
)

func completeToolArguments(arguments string) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal([]byte(arguments), &object) == nil && object != nil
}

func (stream *anthropicInboundStream) needsToolCallSerialization(chunk *llm.Response) bool {
	if chunk == nil || stream.serializedToolTail {
		return false
	}

	activeIndex := stream.currentToolCallIndex
	active := stream.hasCurrentToolCall
	arguments := ""
	if active {
		arguments = stream.toolCalls[activeIndex].Function.Arguments
	}

	for choiceIndex, choice := range chunk.Choices {
		delta := choice.Delta
		if delta == nil {
			continue
		}

		hasContent := lo.FromPtr(delta.Content.Content) != "" || lo.FromPtr(delta.ReasoningContent) != "" ||
			lo.FromPtr(delta.ReasoningSignature) != "" || lo.FromPtr(delta.RedactedReasoningContent) != "" || len(delta.InlineToolResults) > 0
		if active && hasContent && !completeToolArguments(arguments) {
			return true
		}

		for toolIndex, toolCall := range delta.ToolCalls {
			if active && toolCall.Index != activeIndex && !completeToolArguments(arguments) {
				return true
			}
			if !active || toolCall.Index != activeIndex {
				active = true
				activeIndex = toolCall.Index
				arguments = ""
			}
			if toolIndex+1 < len(delta.ToolCalls) || choiceIndex+1 < len(chunk.Choices) {
				arguments += toolCall.Function.Arguments
			}
		}
	}

	return false
}

type interleavedToolBuffer struct {
	chunks []*llm.Response
	calls  map[int]*llm.ToolCall
	bytes  int
}

func (buffer *interleavedToolBuffer) appendChunk(chunk *llm.Response) error {
	if chunk == nil {
		return nil
	}

	encoded, err := json.Marshal(chunk)
	if err != nil {
		return fmt.Errorf("failed to size interleaved tool chunk: %w", err)
	}
	if len(buffer.chunks) >= maxInterleavedToolChunks || len(encoded) > maxInterleavedToolBytes-buffer.bytes {
		return fmt.Errorf("interleaved tool stream exceeds buffer limit")
	}
	buffer.bytes += len(encoded)

	cloned := *chunk
	cloned.Choices = append([]llm.Choice(nil), chunk.Choices...)
	for choiceIndex, choice := range chunk.Choices {
		if choice.Delta == nil {
			continue
		}

		delta := *choice.Delta
		delta.ToolCalls = append([]llm.ToolCall(nil), choice.Delta.ToolCalls...)
		cloned.Choices[choiceIndex].Delta = &delta
		for _, toolCall := range delta.ToolCalls {
			if err := buffer.appendToolCall(toolCall); err != nil {
				return err
			}
		}
	}
	buffer.chunks = append(buffer.chunks, &cloned)

	return nil
}

func (buffer *interleavedToolBuffer) appendToolCall(delta llm.ToolCall) error {
	toolCall := buffer.calls[delta.Index]
	if toolCall == nil {
		cloned := delta
		cloned.Function.Arguments = ""
		cloned.TransformerMetadata = maps.Clone(delta.TransformerMetadata)
		toolCall = &cloned
		buffer.calls[delta.Index] = toolCall
	}

	if delta.ID != "" {
		if toolCall.ID != "" && toolCall.ID != delta.ID {
			return fmt.Errorf("conflicting tool call ID for index %d", delta.Index)
		}
		toolCall.ID = delta.ID
	}
	if delta.Function.Name != "" {
		if toolCall.Function.Name != "" && toolCall.Function.Name != delta.Function.Name {
			return fmt.Errorf("conflicting tool call name for index %d", delta.Index)
		}
		toolCall.Function.Name = delta.Function.Name
	}
	if toolCall.Type == "" {
		toolCall.Type = delta.Type
	}
	if toolCall.Function.Namespace == "" {
		toolCall.Function.Namespace = delta.Function.Namespace
	}
	if delta.Async != nil {
		toolCall.Async = delta.Async
	}
	if len(delta.TransformerMetadata) > 0 {
		if toolCall.TransformerMetadata == nil {
			toolCall.TransformerMetadata = make(map[string]any)
		}
		maps.Copy(toolCall.TransformerMetadata, delta.TransformerMetadata)
	}
	toolCall.Function.Arguments += delta.Function.Arguments

	return nil
}

func (buffer *interleavedToolBuffer) replay(active *llm.ToolCall) ([]*llm.Response, error) {
	for index, toolCall := range buffer.calls {
		arguments := toolCall.Function.Arguments
		if active != nil && index == active.Index {
			arguments = active.Function.Arguments + arguments
		}
		if arguments != "" && !completeToolArguments(arguments) {
			return nil, fmt.Errorf("invalid tool call arguments for tool call index %d", index)
		}
	}

	emitted := make(map[int]bool)
	result := make([]*llm.Response, 0, len(buffer.chunks)+1)
	if active != nil {
		emitted[active.Index] = true
		if continuation := buffer.calls[active.Index]; continuation != nil && continuation.Function.Arguments != "" {
			result = append(result, &llm.Response{
				Choices: []llm.Choice{{Delta: &llm.Message{ToolCalls: []llm.ToolCall{*continuation}}}},
			})
		}
	}

	for _, chunk := range buffer.chunks {
		for choiceIndex := range chunk.Choices {
			delta := chunk.Choices[choiceIndex].Delta
			if delta == nil {
				continue
			}

			toolCalls := delta.ToolCalls
			delta.ToolCalls = nil
			for _, toolCall := range toolCalls {
				if !emitted[toolCall.Index] {
					delta.ToolCalls = append(delta.ToolCalls, *buffer.calls[toolCall.Index])
					emitted[toolCall.Index] = true
				}
			}
		}
		result = append(result, chunk)
	}

	return result, nil
}

func (stream *anthropicInboundStream) serializeInterleavedToolCalls(first *llm.Response) error {
	buffer := interleavedToolBuffer{calls: make(map[int]*llm.ToolCall)}
	var active *llm.ToolCall
	if stream.hasCurrentToolCall {
		active = stream.toolCalls[stream.currentToolCallIndex]
		cloned := *active
		cloned.Function.Arguments = ""
		cloned.TransformerMetadata = maps.Clone(active.TransformerMetadata)
		buffer.calls[active.Index] = &cloned
		buffer.bytes = len(active.Function.Arguments)
	}

	chunk := first
	for {
		if err := buffer.appendChunk(chunk); err != nil {
			return err
		}
		finished := chunk != nil && chunk.Object == "[DONE]"
		if chunk != nil {
			for _, choice := range chunk.Choices {
				finished = finished || choice.FinishReason != nil
			}
		}
		if finished {
			break
		}
		if !stream.source.Next() {
			if err := stream.source.Err(); err != nil {
				return err
			}
			break
		}
		chunk = stream.source.Current()
	}

	chunks, err := buffer.replay(active)
	if err != nil {
		return err
	}
	stream.source = streams.PrependStream(stream.source, chunks...)
	stream.serializedToolTail = true

	return nil
}
