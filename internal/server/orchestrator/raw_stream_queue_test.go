package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestRawStreamQueueReleasesConsumedHeldEvents(t *testing.T) {
	for _, buffered := range []bool{false, true} {
		t.Run(map[bool]string{false: "live", true: "buffered"}[buffered], func(t *testing.T) {
			backlog := &rawStreamBacklog{}
			expected := []*httpclient.StreamEvent{
				{Type: "first", Data: make([]byte, 1024*1024)},
				{Type: "second", Data: make([]byte, 1024*1024)},
			}
			for _, event := range expected {
				_, err := backlog.hold(event)
				require.NoError(t, err)
			}
			queue := newRawStreamQueue(backlog)
			backing := queue.attach()
			for i, want := range expected {
				var got *httpclient.StreamEvent
				var ok bool
				if buffered { got, ok = queue.nextBuffered() } else { got, ok = queue.next(context.Background()) }
				require.True(t, ok)
				require.Same(t, want, got)
				// A consumed slot must not pin its large data through the backing
				// array while the rest of a long-lived stream is still running.
				require.Nil(t, backing[i])
			}
			require.Zero(t, backlog.bytes)
			require.Zero(t, backlog.count)
		})
	}
}
