package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestStreamTerminalTracker_RequiresEveryObservedChoice(t *testing.T) {
	tracker := NewStreamTerminalTracker(2)
	require.False(t, tracker.Observe(&httpclient.StreamEvent{Data: []byte(`{"choices":[{"index":0,"finish_reason":"stop"},{"index":1,"finish_reason":null}]}`)}))
	require.True(t, tracker.Observe(&httpclient.StreamEvent{Data: []byte(`{"choices":[{"index":1,"finish_reason":"stop"}]}`)}))

	unknown := NewStreamTerminalTracker(0)
	require.False(t, unknown.Observe(&httpclient.StreamEvent{Data: []byte(`{"choices":[{"index":0,"finish_reason":"stop"}]}`)}))
	require.False(t, unknown.Observe(&httpclient.StreamEvent{Data: []byte(`{"choices":[{"index":1,"finish_reason":null}]}`)}))
}

func TestStreamTerminalTrackerWaitsForSteeredContinuation(t *testing.T) {
	for _, terminal := range []string{
		`{"type":"response.completed","response":{"id":"resp_first","status":"completed"}}`,
		`{"type":"response.incomplete","response":{"id":"resp_first","status":"incomplete","incomplete_details":{"reason":"steered"}}}`,
	} {
		tracker := NewStreamTerminalTracker(1)
		require.False(t, tracker.Observe(&httpclient.StreamEvent{Data: []byte(`{"type":"response.steer.accepted","steer":{"id":"steer_1","previous_response_id":"resp_first"}}`)}))
		require.False(t, tracker.Observe(&httpclient.StreamEvent{Data: []byte(terminal)}))
		require.False(t, tracker.Observe(&httpclient.StreamEvent{Data: []byte(`{"type":"response.created","response":{"id":"resp_next"}}`)}))
		require.True(t, tracker.Observe(&httpclient.StreamEvent{Data: []byte(`{"type":"response.completed","response":{"id":"resp_next","status":"completed"}}`)}))
	}
}
