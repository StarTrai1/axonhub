package antigravity

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/gemini"
)

var (
	errorProjectRef = regexp.MustCompile(`(?i)\bprojects/[a-z0-9][a-z0-9._:-]*`)
	errorEmail      = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	errorConsumer   = regexp.MustCompile(`(?i)\b(consumer|project(?:[ _-]?(?:id|number))?)(\s*[:=]?\s*['"]?)[0-9]{6,}`)
	errorQuery      = regexp.MustCompile(`(?i)([?&](?:key|api_key|apikey|access_token|refresh_token|token|signature|credential|client_secret)=)[^&\s"'<>]+`)
)

// TransformError keeps the original error available to retry classification and
// execution logs, while exposing only a sanitized message to the client.
func (t *Transformer) TransformError(ctx context.Context, rawErr *httpclient.Error) *llm.ResponseError {
	if rawErr == nil {
		return t.geminiTransformer.TransformError(ctx, nil)
	}

	result := &llm.ResponseError{
		StatusCode: rawErr.StatusCode,
		Detail: llm.ErrorDetail{
			Type: "api_error",
		},
		Cause: rawErr,
	}
	var envelope struct {
		Error    *gemini.ErrorDetail `json:"error"`
		Response *struct {
			Error *gemini.ErrorDetail `json:"error"`
		} `json:"response"`
	}
	if err := json.Unmarshal(rawErr.Body, &envelope); err == nil {
		detail := envelope.Error
		if detail == nil && envelope.Response != nil {
			detail = envelope.Response.Error
		}
		if detail != nil {
			result.Detail.Message = detail.Message
			result.Detail.Type = detail.Status
			result.Detail.Code = strconv.Itoa(detail.Code)
		}
	} else if !json.Valid(rawErr.Body) {
		result.Detail.Message = string(rawErr.Body)
	}
	// Never return a raw JSON envelope when message is absent: details can
	// contain arbitrary account metadata beyond the identifiers scrubbed below.
	if strings.TrimSpace(result.Detail.Message) == "" {
		result.Detail.Message = http.StatusText(rawErr.StatusCode)
		if result.Detail.Message == "" {
			result.Detail.Message = "Upstream request failed"
		}
	}
	result.Detail.Message = sanitizeErrorMessage(result.Detail.Message)
	return result
}

func sanitizeErrorMessage(message string) string {
	message = errorQuery.ReplaceAllString(message, "${1}***")
	message = errorProjectRef.ReplaceAllString(message, "projects/***")
	message = errorEmail.ReplaceAllString(message, "***")
	return errorConsumer.ReplaceAllString(message, "${1}${2}***")
}

// Backend errors can arrive inside a successful HTTP stream. Preserve their
// status for retry/quota handling and use the same client-safe error conversion.
func (t *Transformer) inBandError(ctx context.Context, body []byte) error {
	var envelope struct {
		Error    *gemini.ErrorDetail `json:"error"`
		Response *struct {
			Error *gemini.ErrorDetail `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return nil
	}
	detail := envelope.Error
	if detail == nil && envelope.Response != nil {
		detail = envelope.Response.Error
	}
	if detail == nil {
		return nil
	}
	status := detail.Code
	if status < 400 || status > 599 {
		status = llm.InferResponseErrorStatusCode("", detail.Status, detail.Message)
		if status == 0 {
			status = http.StatusBadGateway
		}
	}
	return t.TransformError(ctx, &httpclient.Error{StatusCode: status, Body: append([]byte(nil), body...)})
}
