package shared

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestResponsesEffortConversionIgnoresDataAndPreservesOverrides(t *testing.T) {
	body := []byte(`{"reasoning":{"effort":"low"},"input":[{"type":"configuration_update","reasoning":{"effort":"high"}},{"type":"configuration_update","reasoning":{"effort":42}},{"type":"configuration_update","reasoning":{"effort":" "}},{"type":"function_call_output","output":[{"type":"configuration_update","reasoning":{"effort":"max"}}]}]}`)
	req := &llm.Request{APIFormat: llm.APIFormatOpenAIResponseWebSocket, ReasoningEffort: "low", RawRequest: &httpclient.Request{Body: body}}
	require.Equal(t, "high", WithResponsesEffortForConversion(req).ReasoningEffort)
	require.Equal(t, "low", req.ReasoningEffort)
	req.ReasoningEffort = "medium"
	require.Same(t, req, WithResponsesEffortForConversion(req))
	req.ReasoningEffort, req.APIFormat = "low", llm.APIFormatOpenAIChatCompletion
	require.Same(t, req, WithResponsesEffortForConversion(req))
}
