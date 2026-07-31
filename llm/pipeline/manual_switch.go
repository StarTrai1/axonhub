package pipeline

import (
	"context"
	"errors"
	"sync"

	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

var (
	// ErrManualChannelSwitch is the cancellation cause used when an operator
	// abandons the current upstream attempt in favor of another channel.
	ErrManualChannelSwitch = errors.New("current upstream attempt canceled for a manual channel switch")
	// ErrManualSwitchClosed indicates that the request is not active on this instance.
	ErrManualSwitchClosed = errors.New("request is no longer active on this AxonHub instance")
	// ErrManualSwitchNotReady indicates that the upstream attempt is between switchable phases.
	ErrManualSwitchNotReady = errors.New("request is not ready to switch channels")
	// ErrManualSwitchCommitted prevents a second response after downstream output starts.
	ErrManualSwitchCommitted = errors.New("response output has already started and cannot be switched safely")
	// ErrManualSwitchInProgress prevents concurrent operator switch requests.
	ErrManualSwitchInProgress = errors.New("a channel switch is already in progress")
	// ErrManualSwitchNoAlternative indicates that the routing plan has no different channel.
	ErrManualSwitchNoAlternative = errors.New("no alternative channel is available for this request")
)

// ManualSwitchable is implemented by outbound transformers that can move an
// in-flight request to the next distinct channel selected by their routing plan.
type ManualSwitchable interface {
	HasAlternativeChannel() bool
	NextAlternativeChannel(ctx context.Context) error
}

// ManualSwitchControl serializes an operator switch request against the point
// where a successful response becomes visible to the downstream client.
type ManualSwitchControl struct {
	mu sync.Mutex

	closed          bool
	ready           bool
	committed       bool
	switchRequested bool
	hasAlternative  bool
	cancelAttempt   func()
}

// NewManualSwitchControl creates a control for one logical downstream request.
func NewManualSwitchControl() *ManualSwitchControl {
	return &ManualSwitchControl{}
}

// BeginAttempt exposes only the current upstream attempt to the control plane.
func (c *ManualSwitchControl) BeginAttempt(cancelAttempt func(), hasAlternative bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}

	c.ready = true
	c.committed = false
	c.switchRequested = false
	c.hasAlternative = hasAlternative
	c.cancelAttempt = cancelAttempt
}

// RequestSwitch atomically wins or loses against TryCommit. The cancellation
// runs after releasing the mutex because it can synchronously unwind transports.
func (c *ManualSwitchControl) RequestSwitch() error {
	c.mu.Lock()

	if c.closed {
		c.mu.Unlock()
		return ErrManualSwitchClosed
	}
	if c.committed {
		c.mu.Unlock()
		return ErrManualSwitchCommitted
	}
	if !c.ready || c.cancelAttempt == nil {
		c.mu.Unlock()
		return ErrManualSwitchNotReady
	}
	if c.switchRequested {
		c.mu.Unlock()
		return ErrManualSwitchInProgress
	}
	if !c.hasAlternative {
		c.mu.Unlock()
		return ErrManualSwitchNoAlternative
	}

	c.switchRequested = true
	cancelAttempt := c.cancelAttempt
	c.mu.Unlock()

	cancelAttempt()

	return nil
}

// TryCommit marks the response boundary as crossed unless an operator already
// requested a switch. Once committed, a second provider response must not be
// appended to the same downstream stream.
func (c *ManualSwitchControl) TryCommit() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed || c.switchRequested {
		return false
	}

	c.committed = true
	c.ready = false
	c.cancelAttempt = nil

	return true
}

// EndAttempt clears the attempt-local callback and reports whether the error
// was caused by an accepted operator switch.
func (c *ManualSwitchControl) EndAttempt() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	requested := c.switchRequested
	c.ready = false
	c.switchRequested = false
	c.hasAlternative = false
	c.cancelAttempt = nil

	return requested
}

// Close permanently removes this logical request from the switchable phase.
func (c *ManualSwitchControl) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closed = true
	c.ready = false
	c.hasAlternative = false
	c.cancelAttempt = nil
}

type manualSwitchCommittedStream struct {
	stream  streams.Stream[*httpclient.StreamEvent]
	cancel  context.CancelCauseFunc
	control *ManualSwitchControl
	once    sync.Once
}

func newManualSwitchCommittedStream(
	stream streams.Stream[*httpclient.StreamEvent],
	cancel context.CancelCauseFunc,
	control *ManualSwitchControl,
) streams.Stream[*httpclient.StreamEvent] {
	return &manualSwitchCommittedStream{
		stream:  stream,
		cancel:  cancel,
		control: control,
	}
}

func (s *manualSwitchCommittedStream) Next() bool {
	return s.stream.Next()
}

func (s *manualSwitchCommittedStream) Current() *httpclient.StreamEvent {
	return s.stream.Current()
}

func (s *manualSwitchCommittedStream) Err() error {
	return s.stream.Err()
}

func (s *manualSwitchCommittedStream) Close() error {
	err := s.stream.Close()
	s.once.Do(func() {
		s.cancel(nil)
		s.control.Close()
	})

	return err
}
