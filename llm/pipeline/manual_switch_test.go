package pipeline

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManualSwitchControl_RequestWinsBeforeCommit(t *testing.T) {
	control := NewManualSwitchControl()
	canceled := make(chan struct{}, 1)
	control.BeginAttempt(func() { canceled <- struct{}{} }, true)

	require.NoError(t, control.RequestSwitch())
	require.False(t, control.TryCommit())
	require.True(t, control.EndAttempt())
	require.Len(t, canceled, 1)
}

func TestManualSwitchControl_CommitRejectsLateSwitch(t *testing.T) {
	control := NewManualSwitchControl()
	control.BeginAttempt(func() {}, true)

	require.True(t, control.TryCommit())
	require.ErrorIs(t, control.RequestSwitch(), ErrManualSwitchCommitted)
}

func TestManualSwitchControl_RequiresDistinctAlternative(t *testing.T) {
	control := NewManualSwitchControl()
	control.BeginAttempt(func() {}, false)

	require.ErrorIs(t, control.RequestSwitch(), ErrManualSwitchNoAlternative)
}
