package gemini

import (
	"net/http"
	"strconv"

	"github.com/looplj/axonhub/llm"
)

// geminiInBandError classifies only an explicit Google error envelope. Model
// finish reasons and prompt safety feedback remain normal protocol results.
func geminiInBandError(detail *ErrorDetail) *llm.ResponseError {
	if detail == nil {
		return nil
	}
	code := ""
	if detail.Code != 0 {
		code = strconv.Itoa(detail.Code)
	}
	status := detail.Code
	if status < http.StatusBadRequest || status > 599 {
		status = llm.InferResponseErrorStatusCode(code, detail.Status, detail.Message)
		if status == 0 {
			status = http.StatusBadGateway
		}
	}
	return &llm.ResponseError{
		StatusCode: status,
		Detail: llm.ErrorDetail{
			Code:    code,
			Type:    detail.Status,
			Message: detail.Message,
		},
	}
}
