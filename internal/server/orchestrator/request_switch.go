package orchestrator

import (
	"sync"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func registerRequestSwitch(state *PersistenceState) {
	if state == nil || state.Request == nil || state.RequestService == nil || state.ManualSwitchControl == nil {
		return
	}

	state.RequestService.RequestSwitchRegistry.Register(state.Request.ID, state.ManualSwitchControl)
}

func unregisterRequestSwitch(state *PersistenceState) {
	if state == nil || state.ManualSwitchControl == nil {
		return
	}

	state.ManualSwitchControl.Close()
	if state.Request == nil || state.RequestService == nil {
		return
	}

	state.RequestService.RequestSwitchRegistry.Unregister(state.Request.ID, state.ManualSwitchControl)
}

type requestSwitchLifecycleStream struct {
	stream streams.Stream[*httpclient.StreamEvent]
	state  *PersistenceState
	once   sync.Once
}

func withRequestSwitchLifecycle(
	stream streams.Stream[*httpclient.StreamEvent],
	state *PersistenceState,
) streams.Stream[*httpclient.StreamEvent] {
	return &requestSwitchLifecycleStream{
		stream: stream,
		state:  state,
	}
}

func (s *requestSwitchLifecycleStream) Next() bool {
	return s.stream.Next()
}

func (s *requestSwitchLifecycleStream) Current() *httpclient.StreamEvent {
	return s.stream.Current()
}

func (s *requestSwitchLifecycleStream) Err() error {
	return s.stream.Err()
}

func (s *requestSwitchLifecycleStream) Close() error {
	err := s.stream.Close()
	s.once.Do(func() { unregisterRequestSwitch(s.state) })

	return err
}
