package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/httpclient"
)

func TestRawRecorderReleasesConsumedEvents(t *testing.T) {
	expected := []*httpclient.StreamEvent{{Type: "first", Data: make([]byte, 1<<20)}, {Type: "second", Data: make([]byte, 1<<20)}}
	recorder := &rawEventRecorder{source: testHTTPStream(expected)}
	require.True(t, recorder.Next())
	require.True(t, recorder.Next())
	backing := recorder.take()
	require.Zero(t, recorder.bytes)
	require.Nil(t, recorder.pending)
	stream := &passThroughStream{recorder: recorder, pending: backing, done: true}
	for i, want := range expected {
		require.True(t, stream.Next())
		require.Same(t, want, stream.Current())
		require.Nil(t, backing[i], "consumed slots must not pin large events")
	}
	require.False(t, stream.Next())
}
