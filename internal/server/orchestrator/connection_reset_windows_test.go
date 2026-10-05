package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestWindowsWebSocketConnectionReset(t *testing.T) {
	for _, code := range []syscall.Errno{syscall.WSAECONNRESET, syscall.WSAECONNABORTED} {
		t.Run(fmt.Sprint(uintptr(code)), func(t *testing.T) {
			failure := fmt.Errorf("websocket dial failed: %w", &net.OpError{
				Op: "read", Net: "tcp", Err: &os.SyscallError{Syscall: "wsarecv", Err: code},
			})
			require.True(t, IsUpstreamTransportError(failure))
			require.True(t, isRetryableErrorForChannel(failure, nil))
			require.True(t, isRetryableTransportError(failure))
			classified := ClassifyUpstreamTransportError(failure)
			var detail *llm.ResponseError
			require.ErrorAs(t, classified, &detail)
			require.Equal(t, http.StatusBadGateway, detail.StatusCode)
			require.Equal(t, ErrCodeUpstreamStreamInterrupted, detail.Detail.Code)
			require.ErrorIs(t, classified, code)
			for _, permanent := range []error{
				errors.Join(context.Canceled, failure),
				errors.Join(context.DeadlineExceeded, failure),
				errors.Join(&httpclient.Error{StatusCode: http.StatusBadRequest}, failure),
				&llm.ResponseError{StatusCode: http.StatusUnauthorized, Cause: failure},
			} {
				require.False(t, IsUpstreamTransportError(permanent))
				require.False(t, isRetryableErrorForChannel(permanent, nil))
			}
		})
	}
	// Proxy authorization failures and locally cancelled I/O are not resets.
	require.False(t, isRetryableErrorForChannel(&os.SyscallError{Syscall: "connectex", Err: syscall.WSAEACCES}, nil))
	require.False(t, isRetryableErrorForChannel(&os.SyscallError{Syscall: "wsarecv", Err: syscall.ERROR_OPERATION_ABORTED}, nil))
}
