package orchestrator

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

const relayCompactionMessage = "上游无法验证该压缩摘要；未删除历史。请从原接入恢复，或重发完整会话历史"

func TestResponsesRelayCompactionRecovery(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, opening := range []bool{false, true} {
			for _, clientIndex := range []bool{false, true} {
				t.Run(fmt.Sprintf("raw=%t/opening=%t/client-index=%t", passthrough, opening, clientIndex), func(t *testing.T) {
					req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
					original := append([]byte(nil), req.Body...)
					ref, _, _, err := parseRemoteCompactionRequest(req.Body)
					require.NoError(t, err)
					owner := &ent.APIKey{ID: 901, ProjectID: 902}
					adapter := newRemoteCompactionAdapter(nil, nil, nil)
					adapter.summaries.SetDefault(remoteCompactionOwnerCacheKey(&PersistenceState{APIKey: owner}, remoteCompactionCacheKey(ref)), "verified retained source summary")
					executor := &relayCompactionExecutor{opening: opening, clientIndex: clientIndex}
					_, result, err := runRejectedReasoningPipeline(t, shared.WithSessionScope(t.Context(), t.Name()), req, executor, t.Name(), passthrough, 1,
						func(_ *PersistenceState, _ *PersistentOutboundTransformer) pipeline.Middleware {
							return pipeline.OnRawRequest("promote-tool-declarations", func(_ context.Context, request *httpclient.Request) (*httpclient.Request, error) {
								body, err := sjson.DeleteBytes(request.Body, "input.0")
								copy := *request
								copy.Body = body
								return &copy, err
							})
						},
						func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
							state.APIKey = owner
							return recoverRejectedRemoteCompaction(outbound, adapter, executor)
						})
					require.NoError(t, err)
					events := drainRejectedReasoningPipeline(t, result)
					require.Contains(t, events, "response.completed")
					require.Len(t, executor.requests, 2)
					assertRejectedCompactionWindowPreserved(t, executor.requests[0].Body, executor.requests[1].Body)
					require.Equal(t, original, req.Body)
				})
			}
		}
	}
}

type relayCompactionExecutor struct {
	responsesReasoningPipelineExecutor
	opening, clientIndex bool
}

func (e *relayCompactionExecutor) DoStream(_ context.Context, req *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
	_ = e.capture(req)
	if len(e.requests) > 1 {
		return streams.SliceStream(rejectedReasoningCompactionEvents()), nil
	}
	index := 1
	if e.clientIndex {
		index = 2
	}
	body := []byte(fmt.Sprintf(`{"error":{"type":"error","code":"unsupported_encrypted_compaction","param":"input[%d].encrypted_content","message":%q}}`, index, relayCompactionMessage))
	if e.opening {
		return nil, &httpclient.Error{StatusCode: 400, Body: body}
	}
	return streams.SliceStream([]*httpclient.StreamEvent{
		{Type: "response.created", Data: []byte(`{"type":"response.created","response":{"id":"resp_failed","object":"response","status":"in_progress","output":[]}}`)},
		{Type: "error", Data: []byte(`{"type":"error","error":` + gjson.GetBytes(body, "error").Raw + `}`)},
	}), nil
}
