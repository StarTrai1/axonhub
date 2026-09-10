package orchestrator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestScheduledHealthRejectsFailedStreamEvenAfterText(testingT *testing.T) {
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
