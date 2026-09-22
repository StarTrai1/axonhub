package httpclient

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/streams"
)

type contextCloseOrderStream struct {
	streams.Stream[*StreamEvent]
	closeFunc func() error
}

type attemptContextTransport struct {
	roundTrip func(*http.Request) (*http.Response, error)
}

func (transport *attemptContextTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport.roundTrip(request)
}

type attemptContextBody struct {
	io.Reader
	closeFunc func() error
}

func (body *attemptContextBody) Close() error {
	return body.closeFunc()
}

func TestHttpClientStreamAttemptCancellationIsIsolated(test *testing.T) {
	var attempts []context.Context
	closeCalls := 0
	client := NewHttpClientWithClient(&http.Client{Transport: &attemptContextTransport{
		roundTrip: func(request *http.Request) (*http.Response, error) {
			ctx := request.Context()
			attempts = append(attempts, ctx)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"text/event-stream"}},
				Body: &attemptContextBody{
					Reader: strings.NewReader("data: done\n\n"),
					closeFunc: func() error {
						require.ErrorIs(test, ctx.Err(), context.Canceled)
						closeCalls++
						return nil
					},
				},
			}, nil
		},
	}})
	var opened []streams.Stream[*StreamEvent]
	for range 2 {
		stream, err := client.DoStream(test.Context(), &Request{Method: http.MethodPost, URL: "https://example.com"})
		require.NoError(test, err)
		test.Cleanup(func() { _ = stream.Close() })
		opened = append(opened, stream)
	}
	require.NoError(test, opened[0].Close())
	require.ErrorIs(test, attempts[0].Err(), context.Canceled)
	require.NoError(test, attempts[1].Err(), "another attempt remains usable")
	require.NoError(test, test.Context().Err(), "the caller can retry with its original context")
	require.True(test, opened[1].Next())
	require.Equal(test, "done", string(opened[1].Current().Data))
	require.False(test, opened[1].Next())
	require.NoError(test, opened[1].Err(), "clean EOF must stay successful")
	require.ErrorIs(test, attempts[1].Err(), context.Canceled)
	require.NoError(test, opened[0].Close())
	require.NoError(test, opened[1].Close())
	require.Equal(test, 2, closeCalls)
}

func TestHttpClientStreamErrorCancelsAttemptBeforeBodyClose(test *testing.T) {
	closed := false
	client := NewHttpClientWithClient(&http.Client{Transport: &attemptContextTransport{
		roundTrip: func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body: &attemptContextBody{
					Reader: strings.NewReader(`{"error":{"message":"try later"}}`),
					closeFunc: func() error {
						require.ErrorIs(test, request.Context().Err(), context.Canceled)
						closed = true
						return nil
					},
				},
			}, nil
		},
	}})
	stream, err := client.DoStream(test.Context(), &Request{Method: http.MethodPost, URL: "https://example.com"})
	require.Nil(test, stream)
	var failure *Error
	require.ErrorAs(test, err, &failure)
	require.Equal(test, http.StatusTooManyRequests, failure.StatusCode)
	require.True(test, closed)
	require.NoError(test, test.Context().Err())
}

func (stream *contextCloseOrderStream) Close() error {
	return stream.closeFunc()
}

func TestRequestContextStreamCancelsBeforeDecoderClose(test *testing.T) {
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
			stream := &requestContextStream{Stream: source, cancel: cancel}
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
