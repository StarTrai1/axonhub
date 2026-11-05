package shared

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHTTPInterruptAttemptRegistration(t *testing.T) {
	control := NewResponsesWebSocketSteering(1)
	oldCleanup := control.RegisterHTTPInterrupt(func(id string) bool { return id == "old" })
	require.True(t, control.InterruptHTTP("old"))
	newCleanup := control.RegisterHTTPInterrupt(func(id string) bool { return id == "new" })
	oldCleanup()
	require.False(t, control.InterruptHTTP("old"))
	require.True(t, control.InterruptHTTP("new"), "an old attempt must not detach its replacement")
	require.False(t, control.Ready(), "HTTP control cannot advertise native steering")
	require.False(t, control.Send([]byte(`{"type":"response.steer"}`)))
	newCleanup()
	require.False(t, control.InterruptHTTP("new"))
}
