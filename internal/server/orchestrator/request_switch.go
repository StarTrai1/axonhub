package orchestrator

import (
	"sync"
 "context"

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
