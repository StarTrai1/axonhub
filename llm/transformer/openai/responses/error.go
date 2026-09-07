package responses

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

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
		detail.Param = response.Error.Param
		if detail.RequestID == "" {
			detail.RequestID = response.Error.RequestID
		}
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
		Type:      string(event.Type),
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
		if event.Error.Param != "" {
			detail.Param = event.Error.Param
		}
		if event.Error.RequestID != "" {
			detail.RequestID = event.Error.RequestID
		}
	}
	if detail.Message == "" {
		detail.Message = "stream error"
	}
	if detail.Code == "" && detail.Type == "" {
		detail.Type = "stream_error"
	}

	result := newProtocolResponseError(detail)
	status := event.Status
	if status == 0 {
		status = event.StatusCode
	}
	if status >= 400 && status <= 599 {
		result.StatusCode = status
	}
	if headers := responseErrorHeaders(event.Headers); len(headers) > 0 {
		body, _ := json.Marshal(struct {
			Error llm.ErrorDetail `json:"error"`
		}{Error: detail})
		result.Cause = &httpclient.Error{StatusCode: result.StatusCode, Headers: headers, Body: body}
	}
	return result
}

func responseErrorHeaders(values map[string]json.RawMessage) http.Header {
	headers := make(http.Header)
	for name, raw := range values {
		if name == "" || len(name) > 128 || len(raw) > 4096 || strings.ContainsAny(name, "\r\n:\t ") || strings.TrimSpace(string(raw)) == "null" {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			var number json.Number
			if json.Unmarshal(raw, &number) != nil {
				var boolean bool
				if json.Unmarshal(raw, &boolean) != nil {
					continue
				}
				value = strconv.FormatBool(boolean)
			} else {
				value = number.String()
			}
		}
		if !strings.ContainsAny(value, "\r\n") {
			headers.Set(name, value)
		}
	}
	return headers
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
