package shared

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResponsesWebSocketTerminalHistoryIsBoundedAndScoped(t *testing.T) {
	var first, other ResponsesWebSocketTerminals
	first.Remember("")
	require.False(t, first.Contains(""))
	first.Remember("old")
	for i := range 128 {
		id := fmt.Sprintf("resp_%d", i)
		first.Remember(id)
		first.Remember(id)
	}
	require.False(t, first.Contains("old"))
	for i := range 128 {
		id := fmt.Sprintf("resp_%d", i)
		require.True(t, first.Contains(id))
		require.False(t, other.Contains(id))
	}
}
