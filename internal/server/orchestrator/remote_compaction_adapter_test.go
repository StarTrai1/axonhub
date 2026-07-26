package orchestrator

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestBuildLocalCompactionRequest(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"before"}]},
			{"type":"compaction_trigger"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"must be removed"}]}
		],
		"stream":false,
		"client_metadata":{
			"thread_id":"thread-1",
			"x-codex-turn-metadata":"{\"compaction\":{\"implementation\":\"responses_compaction_v2\"}}"
		}
	}`)

	got, err := buildLocalCompactionRequest(body)
	require.NoError(t, err)

	envelope, input, err := decodeResponsesInput(got)
	require.NoError(t, err)
	require.Len(t, input, 2)
	require.Equal(t, "message", rawInputItemType(input[1]))
	require.NotContains(t, string(got), remoteCompactionTriggerType)
	require.Contains(t, string(input[1]), "CONTEXT CHECKPOINT COMPACTION")
	require.JSONEq(t, "true", string(envelope["stream"]))

	var metadata map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(envelope["client_metadata"], &metadata))
	var turnMetadata string
	require.NoError(t, json.Unmarshal(metadata["x-codex-turn-metadata"], &turnMetadata))
	require.JSONEq(t, `{"compaction":{"implementation":"responses"}}`, turnMetadata)
}

func TestBuildStandaloneLocalCompactionGenerationRequest(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"before"}]}
		],
		"instructions":"keep this instruction"
	}`)

	got, err := buildLocalCompactionGenerationRequest(body, llm.RequestTypeCompact)
	require.NoError(t, err)

	envelope, input, err := decodeResponsesInput(got)
	require.NoError(t, err)
	require.Len(t, input, 2)
	require.Equal(t, "message", rawInputItemType(input[1]))
	require.Contains(t, string(input[1]), "CONTEXT CHECKPOINT COMPACTION")
	require.JSONEq(t, "false", string(envelope["stream"]))
	require.JSONEq(t, "false", string(envelope["store"]))
	require.JSONEq(t, `"keep this instruction"`, string(envelope["instructions"]))
}

func TestUseResponsesEndpointForLocalCompaction(t *testing.T) {
	local := &ChannelModelsCandidate{
		APIFormat: llm.APIFormatOpenAIResponseCompact.String(),
		Channel: &biz.Channel{Channel: &ent.Channel{
			Type: channel.TypeCodex,
			Policies: objects.ChannelPolicies{
				RemoteCompaction: objects.RemoteCompactionPolicyLocalBridge,
			},
		}},
	}
	native := &ChannelModelsCandidate{
		APIFormat: llm.APIFormatOpenAIResponseCompact.String(),
		Channel: &biz.Channel{Channel: &ent.Channel{
			Type: channel.TypeCodex,
			Policies: objects.ChannelPolicies{
				RemoteCompaction: objects.RemoteCompactionPolicyNative,
			},
		}},
	}

	useResponsesEndpointForLocalCompaction([]*ChannelModelsCandidate{local, native})

	require.Equal(t, llm.APIFormatOpenAIResponse.String(), local.APIFormat)
	require.Equal(t, llm.APIFormatOpenAIResponseCompact.String(), native.APIFormat)
}

func TestReplaceRemoteCompactionWithLocalSummary(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"retained"}]},
			{"id":"cmp_old","type":"compaction","encrypted_content":"old"},
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"context"}]},
			{"id":"cmp_new","type":"compaction","encrypted_content":"new"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		],
		"stream":true,
		"client_metadata":{"thread_id":"thread-1"}
	}`)

	got, err := replaceRemoteCompactionWithLocalSummary(body, "handoff summary")
	require.NoError(t, err)

	_, input, err := decodeResponsesInput(got)
	require.NoError(t, err)
	require.Len(t, input, 4)
	require.Equal(t, []string{"message", "message", "message", "message"}, []string{
		rawInputItemType(input[0]),
		rawInputItemType(input[1]),
		rawInputItemType(input[2]),
		rawInputItemType(input[3]),
	})
	require.Contains(t, string(input[2]), localCompactionSummaryPrefix)
	require.Contains(t, string(input[2]), "handoff summary")
	require.NotContains(t, string(got), `"type":"compaction"`)
	require.Contains(t, string(input[3]), "continue")
}

func TestParseRemoteCompactionRequestUsesLatestItem(t *testing.T) {
	ref, threadID, model, err := parseRemoteCompactionRequest([]byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"id":"cmp_old","type":"compaction","encrypted_content":"old"},
			{"id":"cmp_new","type":"compaction_summary","encrypted_content":"new"}
		],
		"client_metadata":{"thread_id":"thread-1"}
	}`))

	require.NoError(t, err)
	require.Equal(t, "thread-1", threadID)
	require.Equal(t, "gpt-5.6-sol", model)
	require.Equal(t, &remoteCompactionReference{
		ID:               "cmp_new",
		EncryptedContent: "new",
		Index:            1,
	}, ref)
	require.NotEmpty(t, remoteCompactionCacheKey(ref))
}

func TestCompactionResponseAndSummaryExtraction(t *testing.T) {
	response := []byte(`{
		"output":[
			{"id":"cmp_123","type":"compaction","encrypted_content":"opaque"},
			{"type":"message","role":"assistant","content":[
				{"type":"output_text","text":"part one"},
				{"type":"output_text","text":"part two"}
			]}
		]
	}`)

	require.True(t, responseContainsCompactionID(response, "cmp_123"))
	require.False(t, responseContainsCompactionID(response, "cmp_other"))
	require.Equal(t, "part one\npart two", extractAssistantOutputText(response))
}

func TestStoredRemoteCompactionHeaderLookupIsCaseInsensitive(t *testing.T) {
	headers := []byte(`{
		"x-axonhub-remote-compaction-cache":["cache-key"],
		"thread-id":["thread-1"]
	}`)

	require.Equal(t, "cache-key", storedRemoteCompactionCacheKey(headers))
	require.Equal(t, "thread-1", storedRemoteCompactionThreadID(headers))
}

func TestCollectLocalCompactionBridgeStreamStopsAtCompleted(t *testing.T) {
	completed := &httpclient.StreamEvent{
		Type: "response.completed",
		Data: []byte(`{"type":"response.completed","response":{"id":"resp_bridge","status":"completed"}}`),
	}
	trailing := &httpclient.StreamEvent{Type: "response.in_progress", Data: []byte(`{"type":"response.in_progress"}`)}
	perf := &biz.PerformanceRecord{StartTime: time.Now().Add(-time.Second), Stream: true}

	chunks, err := collectLocalCompactionBridgeStream(
		streams.SliceStream([]*httpclient.StreamEvent{completed, trailing}),
		perf,
	)

	require.NoError(t, err)
	require.Equal(t, []*httpclient.StreamEvent{completed}, chunks)
	require.True(t, perf.RequestCompleted)
	require.NotNil(t, perf.FirstTokenTime)
	_, latencyMs, _ := perf.Calculate()
	require.GreaterOrEqual(t, latencyMs, int64(1000))
}

func TestLocalCompactionBridgeStreamReturnsOneCompactionItem(t *testing.T) {
	text := "handoff summary"
	adapter := newRemoteCompactionAdapter(nil, nil, nil)
	ref := &remoteCompactionReference{ID: "cmp_local", EncryptedContent: "opaque-local"}
	generation := &localCompactionGeneration{
		ref:      ref,
		cacheKey: remoteCompactionCacheKey(ref),
		model:    "gpt-5.6-sol",
	}
	source := streams.SliceStream([]*llm.Response{
		{
			ID:    "resp_summary",
			Model: "gpt-5.6-sol",
			Choices: []llm.Choice{{
				Delta: &llm.Message{
					Role:    "assistant",
					Content: llm.MessageContent{Content: &text},
				},
			}},
		},
		llm.DoneResponse,
	})
	stream := newLocalCompactionBridgeStream(source, adapter, generation)

	var events []*llm.Response
	for stream.Next() {
		events = append(events, stream.Current())
	}

	require.NoError(t, stream.Err())
	require.Len(t, events, 3)
	require.Empty(t, events[0].Choices)
	require.Len(t, events[1].Choices, 1)
	require.Equal(t, "stop", *events[1].Choices[0].FinishReason)
	parts := events[1].Choices[0].Delta.Content.MultipleContent
	require.Len(t, parts, 1)
	require.Equal(t, remoteCompactionItemType, parts[0].Type)
	require.Equal(t, ref.ID, parts[0].Compact.ID)
	require.Same(t, llm.DoneResponse, events[2])
	cached, ok := adapter.summaries.Get(generation.cacheKey)
	require.True(t, ok)
	require.Equal(t, text, cached)
}

func TestLocalCompactionBridgeStreamProducesCodexCompletionContract(t *testing.T) {
	text := "handoff summary"
	adapter := newRemoteCompactionAdapter(nil, nil, nil)
	ref := &remoteCompactionReference{ID: "cmp_local", EncryptedContent: "opaque-local"}
	generation := &localCompactionGeneration{
		ref:      ref,
		cacheKey: remoteCompactionCacheKey(ref),
		model:    "gpt-5.6-sol",
	}
	source := streams.SliceStream([]*llm.Response{
		{
			ID:    "resp_summary",
			Model: "gpt-5.6-sol",
			Choices: []llm.Choice{{Delta: &llm.Message{
				Role:    "assistant",
				Content: llm.MessageContent{Content: &text},
			}}},
		},
		llm.DoneResponse,
	})
	bridge := newLocalCompactionBridgeStream(source, adapter, generation)
	stream, err := responses.NewInboundTransformer().TransformStream(t.Context(), bridge)
	require.NoError(t, err)

	var eventTypes []string
	compactionDone := 0
	completedOutput := 0
	for stream.Next() {
		var event struct {
			Type string `json:"type"`
			Item *struct {
				Type string `json:"type"`
			} `json:"item"`
			Response *struct {
				Output []struct {
					Type string `json:"type"`
				} `json:"output"`
			} `json:"response"`
		}
		require.NoError(t, json.Unmarshal(stream.Current().Data, &event))
		eventTypes = append(eventTypes, event.Type)
		if event.Type == "response.output_item.done" && event.Item != nil && event.Item.Type == remoteCompactionItemType {
			compactionDone++
		}
		if event.Type == "response.completed" && event.Response != nil {
			for _, item := range event.Response.Output {
				if item.Type == remoteCompactionItemType {
					completedOutput++
				}
			}
		}
	}

	require.NoError(t, stream.Err())
	require.NotEmpty(t, eventTypes)
	require.Equal(t, "response.completed", eventTypes[len(eventTypes)-1])
	require.Equal(t, 1, compactionDone)
	require.Equal(t, 1, completedOutput)
}

func TestLocalStandaloneCompactionResponseContainsOnlyOpaqueReference(t *testing.T) {
	ref := &remoteCompactionReference{ID: "cmp_local", EncryptedContent: "opaque-local"}
	generation := &localCompactionGeneration{
		ref:          ref,
		model:        "gpt-5.6-sol",
		instructions: "keep this instruction",
		standalone:   true,
	}
	source := &llm.Response{ID: "resp_summary", Created: 123, Model: "gpt-5.6-sol"}

	got := localStandaloneCompactionResponse(source, generation)

	require.Equal(t, llm.RequestTypeCompact, got.RequestType)
	require.Equal(t, "response.compaction", got.Compact.Object)
	require.Equal(t, "keep this instruction", got.Compact.Instructions)
	require.Len(t, got.Compact.Output, 1)
	parts := got.Compact.Output[0].Content.MultipleContent
	require.Len(t, parts, 1)
	require.Equal(t, remoteCompactionItemType, parts[0].Type)
	require.Equal(t, ref, &remoteCompactionReference{
		ID:               parts[0].Compact.ID,
		EncryptedContent: parts[0].Compact.EncryptedContent,
	})
}
