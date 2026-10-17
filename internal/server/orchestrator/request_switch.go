package orchestrator

import (
	"context"
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
	memoryTerminal *StreamTerminalTracker
}

func withRequestSwitchLifecycle(
	stream streams.Stream[*httpclient.StreamEvent],
	state *PersistenceState,
) streams.Stream[*httpclient.StreamEvent] {
	wrapped := &requestSwitchLifecycleStream{
		stream: stream,
		state:  state,
	}
	if state.codexMemory != nil {
		wrapped.memoryTerminal = NewStreamTerminalTracker(1)
	}
	return wrapped
}

func (s *requestSwitchLifecycleStream) Next() bool {
	next := s.stream.Next()
	if !next && s.stream.Err() != nil && !s.DeliveryAcknowledged() {
		s.state.codexMemory.finish(false)
	}
	return next
}

func (s *requestSwitchLifecycleStream) Current() *httpclient.StreamEvent {
	event := s.stream.Current()
	if s.memoryTerminal != nil && event != nil && s.memoryTerminal.Observe(event) && classifyAcceptedTerminalEvent(event) != streamTerminalCompleted {
		s.state.codexMemory.finish(false)
	}
	return event
}

func (s *requestSwitchLifecycleStream) Err() error {
	return s.stream.Err()
}

func (s *requestSwitchLifecycleStream) Close() error {
	err := s.stream.Close()
	s.once.Do(func() {
		s.state.codexMemory.finish(s.DeliveryAcknowledged() || s.state.StreamCompleted)
		unregisterRequestSwitch(s.state)
	})

	return err
}

func (s *requestSwitchLifecycleStream) AcknowledgeDelivery() {
	if observer, ok := s.stream.(streams.DeliveryAcknowledger); ok {
		observer.AcknowledgeDelivery()
	}
}

func (s *requestSwitchLifecycleStream) DeliveryAcknowledged() bool {
	observer, ok := s.stream.(streams.DeliveryAcknowledger)
	return ok && observer.DeliveryAcknowledged()
}

func (s *requestSwitchLifecycleStream) WaitForFinalization() {
	if waiter, ok := s.stream.(streams.FinalizationWaiter); ok {
		waiter.WaitForFinalization()
	}
}

func (s *requestSwitchLifecycleStream) Deliver(ctx context.Context, write func() (bool, error)) error {
	boundary, ok := s.stream.(streams.DeliveryBoundary)
	if ok {
		return boundary.Deliver(ctx, write)
	}
	terminal, err := write()
	if err == nil && terminal {
		s.AcknowledgeDelivery()
	}
	return err
}

func (s *requestSwitchLifecycleStream) ExpectedStreamChoices() int {
	choices, ok := s.stream.(interface{ ExpectedStreamChoices() int })
	if !ok {
		return 0
	}
	return choices.ExpectedStreamChoices()
}
