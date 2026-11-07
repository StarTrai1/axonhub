package plugin

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
)

func TestHookProjectionPreservesPrivateNativeDecisions(t *testing.T) {
	request := &llm.Request{Model: "model", Decisions: json.RawMessage(`{"questions":[{"id":"private-order","extension":9007199254740993}]}`)}
	projected, err := hookRequestJSON(request)
	require.NoError(t, err)
	require.NotContains(t, string(projected), "private-order")
	original := string(request.Decisions)
	replacePublicRequest(request, llm.Request{Model: "model"})
	require.Equal(t, original, string(request.Decisions), "public hook projection must retain ordered native payload verbatim")
}
