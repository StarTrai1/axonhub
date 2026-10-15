package gemini

import (
	"net/http"
	"strconv"

	"github.com/looplj/axonhub/llm"
)

// geminiInBandError classifies an explicit Google error envelope.
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

// A malformed call without a usable tool call is a generation failure, not a
// safety refusal. Track calls per candidate so a call in another choice cannot
// hide the failure, and a call already delivered in a stream is not replayed.
func geminiMalformedToolError(response *GenerateContentResponse, seen map[int64]bool) *llm.ResponseError {
	for _, candidate := range response.Candidates {
		if candidate == nil {
			continue
		}
		hasCall := seen[candidate.Index]
		if candidate.Content != nil {
			for _, part := range candidate.Content.Parts {
				if part != nil && part.FunctionCall != nil {
					hasCall = true
				}
			}
		}
		if hasCall && seen != nil {
			seen[candidate.Index] = true
		}
		if candidate.FinishReason == "MALFORMED_FUNCTION_CALL" && !hasCall {
			return &llm.ResponseError{
				StatusCode: http.StatusBadGateway,
				Detail: llm.ErrorDetail{
					Code: "MALFORMED_FUNCTION_CALL",
					Type: "api_error",
					Message: "Gemini response finished with MALFORMED_FUNCTION_CALL",
				},
			}
		}
	}
	return nil
}
