package pipeline

import (
	"errors"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func isMissingGeminiCachedContent(err error) bool {
	if httpclient.IsMissingGeminiCachedContent(err) {
		return true
	}
	var response *llm.ResponseError
	return errors.As(err, &response) && httpclient.IsMissingGeminiCachedContentMessage(response.StatusCode, response.Detail.Message)
}
