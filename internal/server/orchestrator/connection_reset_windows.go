package orchestrator

import (
	"errors"
	"syscall"
)

// Winsock errors from wsarecv/wsasend are distinct from Go's compatibility
// ECONNRESET/ECONNABORTED values, and read failures are not net.Error.Temporary.
func isPlatformConnectionResetError(err error) bool {
	return errors.Is(err, syscall.WSAECONNRESET) || errors.Is(err, syscall.WSAECONNABORTED)
}
