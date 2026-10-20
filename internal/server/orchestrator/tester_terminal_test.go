package orchestrator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestChannelRejectsFailedStreamEvenAfterText(testingT *testing.T) {
	for _, reason := range []string{"error", "cancelled", "canceled"} {
		testingT.Run(reason, func(testingT *testing.T) {
			processor := &TestChannelOrchestrator{}
			stream := streams.SliceStream([]*httpclient.StreamEvent{
				{Data: []byte(`{"choices":[{"delta":{"content":"partial"}}]}`)},
				{Data: []byte(`{"choices":[{"delta":{},"finish_reason":"` + reason + `"}]}`)},
				{Data: []byte(`{"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":5,"total_tokens":16}}`)},
				{Data: []byte("[DONE]")},
			})
			result, err := processor.handleStreamResponse(testingT.Context(), stream, time.Now())
			require.NoError(testingT, err)
			require.False(testingT, result.Success)
			require.Contains(testingT, *result.Error, reason)
			require.Equal(testingT, "partial", *result.Message)
		})
	}
}

func TestChannelNativeResponsesProbeRequiresSuccessfulTerminal(t *testing.T) {
	for _, status := range []string{"completed", "failed", "incomplete"} {
		t.Run(status, func(t *testing.T) {
			stream := streams.SliceStream([]*httpclient.StreamEvent{
				{Data: []byte(`{"type":"response.output_text.delta","delta":"hello"}`)},
				{Data: []byte(`{"type":"response.` + status + `","response":{"status":"` + status + `","output":[]}}`)},
			})
			result, err := (&TestChannelOrchestrator{}).handleStreamResponse(t.Context(), stream, time.Now())
			require.NoError(t, err)
			require.Equal(t, status == "completed", result.Success)
			require.Equal(t, "hello", *result.Message)
		})
	}
}
