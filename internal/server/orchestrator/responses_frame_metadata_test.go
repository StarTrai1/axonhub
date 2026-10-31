package orchestrator

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

func TestResponsesFrameMetadataReplacesStaleHandshake(t *testing.T) {
	request := &httpclient.Request{APIFormat: string(llm.APIFormatOpenAIResponse), Headers:http.Header{
		"Session-Id":{"root"}, "Thread-Id":{"old-thread"}, "X-Codex-Window-Id":{"old-thread:0"}, "X-Codex-Turn-Metadata":{`{"request_kind":"prewarm"}`},
	}, Body:[]byte(`{"client_metadata":{"session_id":"root","thread_id":"child","x-codex-window-id":"child:4","x-codex-turn-metadata":"{\"session_id\":\"root\",\"thread_id\":\"child\",\"window_id\":\"child:4\",\"request_kind\":\"turn\"}"},"input":[{"role":"user","content":"preserve"}]}`)}
	originalHeaders := request.Headers.Clone()
	result, err := applyResponsesFrameMetadata().OnOutboundRawRequest(shared.WithResponsesWebSocket(t.Context()), request)
	require.NoError(t, err)
	require.Equal(t, "child", result.Headers.Get("Thread-Id"))
	require.Equal(t, "child:4", result.Headers.Get("X-Codex-Window-Id"))
	require.Contains(t, result.Headers.Get("X-Codex-Turn-Metadata"), `"request_kind":"turn"`)
	require.Equal(t, request.Body, result.Body)
	require.Equal(t, originalHeaders, request.Headers)
	result, err = applyResponsesFrameMetadata().OnOutboundRawRequest(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, originalHeaders, result.Headers)
}
