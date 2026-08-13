package responses

import "github.com/looplj/axonhub/llm"

func responseErrorFromResponse(response *Response) *llm.ResponseError {
	if response == nil {
		return newProtocolResponseError(llm.ErrorDetail{
			Code:    "server_error",
			Message: "response failed",
			Type:    "server_error",
		})
	}

	detail := llm.ErrorDetail{RequestID: response.RequestID}
	if response.Error != nil {
		detail.Code = response.Error.Code
		detail.Message = response.Error.Message
		detail.Type = response.Error.Type
	}
	if detail.Message == "" {
		detail.Message = "response failed"
	}
	if detail.Code == "" && detail.Type == "" {
		detail.Code = "server_error"
		detail.Type = "server_error"
	}

	return newProtocolResponseError(detail)
}

func responseErrorFromStreamEvent(event *StreamEvent) *llm.ResponseError {
	if event == nil {
		return responseErrorFromResponse(nil)
	}

	detail := llm.ErrorDetail{
		Code:      event.Code,
		Message:   event.Message,
		RequestID: event.RequestID,
	}
	if event.Param != nil {
		detail.Param = *event.Param
	}
	if event.Error != nil {
		if event.Error.Code != "" {
			detail.Code = event.Error.Code
		}
		if event.Error.Message != "" {
			detail.Message = event.Error.Message
		}
		if event.Error.Type != "" {
			detail.Type = event.Error.Type
		}
	}
	if detail.Message == "" {
		detail.Message = "stream error"
	}
	if detail.Code == "" && detail.Type == "" {
		detail.Type = "stream_error"
	}

	return newProtocolResponseError(detail)
}

func newProtocolResponseError(detail llm.ErrorDetail) *llm.ResponseError {
	statusCode := llm.InferResponseErrorStatusCode(detail.Code, detail.Type, detail.Message)
	if statusCode == 0 {
		statusCode = 502
	}

	return &llm.ResponseError{
		StatusCode: statusCode,
		Detail:     detail,
	}
}
