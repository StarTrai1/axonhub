package biz

import (
	"sync"

	"github.com/looplj/axonhub/llm/pipeline"
)

// RequestSwitchRegistry maps persisted request IDs to controls for requests
// currently executing on this AxonHub instance.
type RequestSwitchRegistry struct {
	controls sync.Map // map[int]*pipeline.ManualSwitchControl
}

// NewRequestSwitchRegistry creates an in-memory registry for active requests.
func NewRequestSwitchRegistry() *RequestSwitchRegistry {
	return &RequestSwitchRegistry{}
}

// Register exposes an active request to the single-instance control plane.
func (r *RequestSwitchRegistry) Register(requestID int, control *pipeline.ManualSwitchControl) {
	if requestID <= 0 || control == nil {
		return
	}

	r.controls.Store(requestID, control)
}

// Unregister removes a control only when it still belongs to the same request execution.
func (r *RequestSwitchRegistry) Unregister(requestID int, control *pipeline.ManualSwitchControl) {
	if requestID <= 0 || control == nil {
		return
	}

	r.controls.CompareAndDelete(requestID, control)
}

// Switch requests cancellation of the current attempt before its response is committed.
func (r *RequestSwitchRegistry) Switch(requestID int) error {
	value, ok := r.controls.Load(requestID)
	if !ok {
		return pipeline.ErrManualSwitchClosed
	}

	control, ok := value.(*pipeline.ManualSwitchControl)
	if !ok || control == nil {
		return pipeline.ErrManualSwitchClosed
	}

	return control.RequestSwitch()
}
