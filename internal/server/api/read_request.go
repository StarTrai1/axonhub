package api

import (
 "errors"
 "net/http"

 "github.com/looplj/axonhub/internal/log"
 "github.com/looplj/axonhub/llm"
 "github.com/looplj/axonhub/llm/httpclient"
)

// readClientRequest classifies failures before the provider pipeline starts.
// Every caller must return on failure: the body may already be consumed.
func readClientRequest(request *http.Request) (*httpclient.Request, error) {
 parsed, err := httpclient.ReadHTTPRequest(request)
 if err == nil {
  return parsed, nil
 }
 status := http.StatusBadRequest
 var sizeError *http.MaxBytesError
 if errors.Is(err, httpclient.ErrRequestBodyTooLarge) || errors.As(err, &sizeError) {
  status = http.StatusRequestEntityTooLarge
 }
 log.Warn(request.Context(), "Failed to read client request", log.Cause(err))
 return nil, &llm.ResponseError{
  StatusCode: status,
  Detail: llm.ErrorDetail{Type: "invalid_request_error", Message: err.Error()},
  Cause: err,
 }
}
