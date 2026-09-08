package httpclient

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/streams"
)

type contextCloseOrderStream struct {
	streams.Stream[*StreamEvent]
	closeFunc func() error
}

func (stream *contextCloseOrderStream) Close() error {
	return stream.closeFunc()
}

func TestDetachedContextStreamCancelsBeforeDecoderClose(test *testing.T) {
	test.Parallel()

	for _, exhaustSource := range []bool{false, true} {
		name := "explicit close"
		if exhaustSource {
			name = "source exhausted"
		}
		test.Run(name, func(test *testing.T) {
			test.Parallel()
			ctx, cancel := context.WithCancel(test.Context())
			defer cancel()
			closeFailure := errors.New("decoder close failure")
			closeCalls := 0
			source := &contextCloseOrderStream{
				Stream: streams.SliceStream([]*StreamEvent{}),
				closeFunc: func() error {
					closeCalls++
					require.ErrorIs(test, ctx.Err(), context.Canceled)
					return closeFailure
				},
			}
			stream := &detachedContextStream{Stream: source, cancel: cancel}
			if exhaustSource {
				require.False(test, stream.Next())
			}
			require.ErrorIs(test, stream.Close(), closeFailure)
			require.ErrorIs(test, stream.Close(), closeFailure)
			require.Equal(test, 1, closeCalls)
			require.NoError(test, test.Context().Err())
		})
	}
}
