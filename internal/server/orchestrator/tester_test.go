package orchestrator

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm/transformer/openai/codex"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestBuildChannelTestRequestResponsesCompatibility(t *testing.T) {
	tests := []struct {
		name                string
		model               string
		requestedStream     bool
		wantStream          bool
		wantCodexHeaders    bool
		wantResponsesLite   bool
		wantReasoningEffort string
	}{
		{
			name:                "gpt-5.5 uses standard streamed responses identity",
			model:               "gpt-5.5",
			requestedStream:     false,
			wantStream:          true,
			wantCodexHeaders:    true,
			wantReasoningEffort: "low",
		},
		{
			name:                "gpt-5.6 sol uses responses lite",
			model:               "gpt-5.6-sol",
			requestedStream:     false,
			wantStream:          true,
			wantCodexHeaders:    true,
			wantResponsesLite:   true,
			wantReasoningEffort: "low",
		},
		{
			name:            "generic model keeps channel stream policy",
			model:           "generic-model",
			requestedStream: false,
			wantStream:      false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := buildChannelTestRequest(test.model, test.requestedStream, "system prompt", "user prompt")
			require.NoError(t, err)
			require.Equal(t, "application/json", request.Headers.Get("Content-Type"))
			require.Equal(t, test.model, gjson.GetBytes(request.Body, "model").String())
			require.Equal(t, "system prompt", gjson.GetBytes(request.Body, "messages.0.content").String())
			require.Equal(t, "system", gjson.GetBytes(request.Body, "messages.0.role").String())
			require.Equal(t, "user prompt", gjson.GetBytes(request.Body, "messages.1.content").String())
			require.Equal(t, "user", gjson.GetBytes(request.Body, "messages.1.role").String())
			require.Equal(t, 2, int(gjson.GetBytes(request.Body, "messages.#").Int()))
			require.Equal(t, channelTestMaxCompletionTokens, gjson.GetBytes(request.Body, "max_completion_tokens").Int())
			require.Equal(t, test.wantStream, gjson.GetBytes(request.Body, "stream").Bool())
			require.False(t, gjson.GetBytes(request.Body, "store").Bool())

			if !test.wantCodexHeaders {
				require.Empty(t, request.Headers.Get("User-Agent"))
				require.Empty(t, request.Headers.Get("Originator"))
				require.Empty(t, request.Metadata)
				return
			}

			require.Equal(t, "text/event-stream", request.Headers.Get("Accept"))
			require.Equal(t, channelTestOriginator, request.Headers.Get("Originator"))
			require.Equal(t, channelTestUserAgent, request.Headers.Get("User-Agent"))
			require.Equal(t, "true", request.Metadata[channelTestRequestMetadataKey])
			require.NotEmpty(t, request.Headers.Get(codex.SessionHeaderHyphen))
			require.Equal(t, request.Headers.Get(codex.SessionHeaderHyphen), request.Headers.Get("Thread-Id"))
			require.Equal(t, request.Headers.Get(codex.SessionHeaderHyphen), request.Headers.Get(codex.ClientRequestIDHeader))
			require.Equal(t, test.wantReasoningEffort, gjson.GetBytes(request.Body, "reasoning_effort").String())
			require.Equal(t, "low", gjson.GetBytes(request.Body, "verbosity").String())
			require.False(t, gjson.GetBytes(request.Body, "parallel_tool_calls").Bool())
			require.Equal(t, request.Headers.Get(codex.SessionHeaderHyphen), gjson.GetBytes(request.Body, "prompt_cache_key").String())

			var metadata channelTestTurnMetadata
			require.NoError(t, json.Unmarshal([]byte(request.Headers.Get(codex.TurnMetadataHeader)), &metadata))
			require.Equal(t, request.Headers.Get(codex.SessionHeaderHyphen), metadata.SessionID)
			require.Equal(t, metadata.SessionID, metadata.ThreadID)
			require.Equal(t, metadata.WindowID, request.Headers.Get(codex.WindowIDHeader))
			require.Equal(t, "turn", metadata.RequestKind)
			require.Equal(t, "user", metadata.ThreadSource)
			require.Equal(t, "none", metadata.Sandbox)
			require.NotZero(t, metadata.TurnStartedAtUnixMS)

			if test.wantResponsesLite {
				require.Equal(t, "true", request.Headers.Get(responses.ResponsesLiteHeader))
				require.Equal(t, "auto", gjson.GetBytes(request.Body, "tool_choice").String())
			} else {
				require.Empty(t, request.Headers.Get(responses.ResponsesLiteHeader))
				require.False(t, gjson.GetBytes(request.Body, "tool_choice").Exists())
			}
		})
	}
}

func TestNormalizeChannelTestModel(t *testing.T) {
	require.True(t, isCodexStyleTestModel("provider/gpt-5.6-luna"))
	require.True(t, isResponsesLiteTestModel("GPT-5.6-TERRA"))
	require.False(t, isCodexStyleTestModel("gpt-5.5-pro"))
	require.False(t, isResponsesLiteTestModel("gpt-5.5"))
}

func TestBuildChannelTestRequestOmitsEmptySystemPrompt(t *testing.T) {
	request, err := buildChannelTestRequest("generic-model", false, "", "user prompt")
	require.NoError(t, err)
	require.Equal(t, 1, int(gjson.GetBytes(request.Body, "messages.#").Int()))
	require.Equal(t, "user", gjson.GetBytes(request.Body, "messages.0.role").String())
	require.Equal(t, "user prompt", gjson.GetBytes(request.Body, "messages.0.content").String())
}

func TestBuildRemoteCompactionChannelTestRequestUsesNativeV2Shape(t *testing.T) {
	request, err := buildRemoteCompactionChannelTestRequest("gpt-5.6-sol", "system prompt", "user prompt")
	require.NoError(t, err)

	require.Equal(t, "gpt-5.6-sol", gjson.GetBytes(request.Body, "model").String())
	require.Equal(t, "system prompt", gjson.GetBytes(request.Body, "instructions").String())
	require.Equal(t, "user prompt", gjson.GetBytes(request.Body, "input.0.content.0.text").String())
	require.Equal(t, remoteCompactionTriggerType, gjson.GetBytes(request.Body, "input.1.type").String())
	require.True(t, gjson.GetBytes(request.Body, "stream").Bool())
	require.False(t, gjson.GetBytes(request.Body, "store").Bool())
	require.Contains(t, request.Headers.Get(codex.BetaFeaturesHeader), "remote_compaction_v2")
	require.Equal(t, "compaction", gjson.Get(request.Headers.Get(codex.TurnMetadataHeader), "request_kind").String())
	require.Equal(t, "capability_probe", gjson.Get(request.Headers.Get(codex.TurnMetadataHeader), "compaction.reason").String())
}

func TestResponseBodyContainsCompactionItem(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "output item event", body: `{"type":"response.output_item.done","item":{"type":"compaction","id":"cmp_1"}}`, want: true},
		{name: "terminal output", body: `{"type":"response.completed","response":{"output":[{"type":"compaction_summary","id":"cmp_2"}]}}`, want: true},
		{name: "json output", body: `{"output":[{"type":"compaction","id":"cmp_3"}]}`, want: true},
		{name: "complete SSE body", body: "data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"compaction\",\"id\":\"cmp_4\"}}\n\ndata: [DONE]\n\n", want: true},
		{name: "message only", body: `{"type":"response.output_item.done","item":{"type":"message","id":"msg_1"}}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, responseBodyContainsCompactionItem([]byte(test.body)))
		})
	}
}
