package orchestrator

import (
	"fmt"
	"net/http"

	"github.com/looplj/axonhub/llm"
)

func NewQuotaReserveError(modelName string) error {
	return &llm.ResponseError{
		StatusCode: http.StatusServiceUnavailable,
		Detail: llm.ErrorDetail{
			Message: fmt.Sprintf("no channel available under configured quota reserve thresholds for model %s", modelName),
			Type:    "quota_routing_error",
			Code:    "quota_reserve_reached",
		},
	}
}

// QuotaExhaustedError is returned when all channels are quota exhausted for a model.
type QuotaExhaustedError struct {
	ModelName string
}

func (e *QuotaExhaustedError) Error() string {
	return fmt.Sprintf("all channels quota exhausted for model %s", e.ModelName)
}

func NewQuotaExhaustedError(modelName string) error {
	return &QuotaExhaustedError{ModelName: modelName}
}

// StreamPolicyConflictError indicates that matching channels require streaming
// for a request type that cannot be auto-aggregated.
type StreamPolicyConflictError struct {
	ModelName string
}

func (e *StreamPolicyConflictError) Error() string {
	return fmt.Sprintf(
		"no eligible channel for model %s: matching channels require streaming, but Decisions requests are non-streaming",
		e.ModelName,
	)
}

func NewStreamPolicyConflictError(modelName string) error {
	return &StreamPolicyConflictError{ModelName: modelName}
}
