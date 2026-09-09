package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestResponsesSteeringTerminalKeepsSuccessorOutcome(testingT *testing.T) {
	steering := newResponsesSteeringState()
	for _, step := range []struct {
		body string
		want streamTerminalState
	}{
		{`{"type":"response.steer.accepted","steer":{"id":"steer_1","previous_response_id":"resp_initial"}}`, streamTerminalNone},
		{`{"type":"response.incomplete","response":{"id":"resp_initial","status":"incomplete","incomplete_details":{"reason":"steered"}}}`, streamTerminalNone},
		{`{"type":"response.created","response":{"id":"resp_successor"}}`, streamTerminalNone},
		{`{"type":"response.failed","response":{"id":"resp_successor","status":"failed","error":{"message":"upstream failed"}}}`, streamTerminalFailed},
	} {
		require.Equal(testingT, step.want, steering.classifyFinalTerminal(&httpclient.StreamEvent{Data: []byte(step.body)}))
	}
}

func TestResponsesSteeringPendingPreservesCompletedBoundary(testingT *testing.T) {
	steering := newResponsesSteeringState()
	require.Equal(testingT, streamTerminalNone, steering.classifyFinalTerminal(&httpclient.StreamEvent{
		Data: []byte(`{"type":"response.steer.accepted","steer":{"id":"steer_1","previous_response_id":"resp_initial"}}`),
	}))
	require.Equal(testingT, streamTerminalNone, steering.classifyFinalTerminal(&httpclient.StreamEvent{
		Data: []byte(`{"type":"response.completed","response":{"id":"resp_initial","status":"completed"}}`),
	}))
	require.Equal(testingT, streamTerminalCompleted, steering.classifyFinalTerminal(&httpclient.StreamEvent{
		Data: []byte(`{"type":"response.steer.pending","steer":{"previous_response_id":"resp_initial"}}`),
	}))
}
