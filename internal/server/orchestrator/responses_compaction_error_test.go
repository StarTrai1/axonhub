package orchestrator

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

const rejectedNativeCompactionMessage = "The encrypted content for item cmp_native could not be verified. Reason: Encrypted content could not be decrypted or parsed."

func rejectedNativeCompactionError() error {
	return &httpclient.Error{StatusCode: http.StatusBadRequest, Body: []byte(`{"error":{"code":"invalid_encrypted_content","message":"` + rejectedNativeCompactionMessage + `"}}`)}
}

func TestResponsesRejectedCompactionDecryptionRequiresExactItem(t *testing.T) {
	body := []byte(responsesCompactionHistoryFixture)
	ref, _, _, err := parseRemoteCompactionRequest(body)
	require.NoError(t, err)
	for _, tc := range []struct {
		name, code, message, param string
		want                       bool
	}{
		{name: "native", code: "invalid_encrypted_content", message: rejectedNativeCompactionMessage, want: true},
		{name: "relay signature code", code: "thinking_signature_invalid", message: rejectedNativeCompactionMessage, want: true},
		{name: "relay signature indexed", code: "thinking_signature_invalid", message: rejectedNativeCompactionMessage, param: "input[2].encrypted_content", want: true},
		{name: "relay signature without exact message", code: "thinking_signature_invalid", message: "Invalid thinking signature", param: "input[2].encrypted_content"},
		{name: "relay signature unknown item", code: "thinking_signature_invalid", message: strings.ReplaceAll(rejectedNativeCompactionMessage, "cmp_native", "cmp_other")},
		{name: "relay signature wrong index", code: "thinking_signature_invalid", message: rejectedNativeCompactionMessage, param: "input[1].encrypted_content"},
		{name: "relay without code", message: rejectedNativeCompactionMessage, want: true},
		{name: "relay wrapper", code: "bad_request", message: "OpenAI Responses bad request: " + rejectedNativeCompactionMessage + " [trace_id=synthetic]", want: true},
		{name: "indexed", code: "invalid_request_error", message: rejectedNativeCompactionMessage, param: "input[2].encrypted_content", want: true},
		{name: "wrong index", message: rejectedNativeCompactionMessage, param: "input[1].encrypted_content"},
		{name: "wrong field", message: rejectedNativeCompactionMessage, param: "input[2].id"},
		{name: "other checkpoint", message: strings.ReplaceAll(rejectedNativeCompactionMessage, "cmp_native", "cmp_other")},
		{name: "reasoning item", message: strings.ReplaceAll(rejectedNativeCompactionMessage, "cmp_native", "rs_native")},
		{name: "permission", code: "permission_denied", message: rejectedNativeCompactionMessage},
		{name: "generic encryption", code: "invalid_encrypted_content", message: "invalid encrypted content"},
		{name: "extra error", message: rejectedNativeCompactionMessage + " Another error occurred."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, responsesRejectedCompactionMessage(body, ref, tc.code, tc.message, tc.param))
		})
	}
	duplicate, err := sjson.SetRawBytes(body, "input.-1", []byte(`{"type":"message","id":"cmp_native","role":"user","content":"unrelated"}`))
	require.NoError(t, err)
	require.False(t, responsesRejectedCompactionMessage(duplicate, ref, "", rejectedNativeCompactionMessage, ""))
}

func TestResponsesRejectedCompactionDecryptionPipelinePreservesWindow(t *testing.T) {
	for _, format := range []llm.APIFormat{llm.APIFormatOpenAIResponse, llm.APIFormatOpenAIResponseCompact} {
		for _, passThrough := range []bool{true, false} {
			name := string(format) + "/converted"
			if passThrough {
				name = string(format) + "/pass-through"
			}
			t.Run(name, func(t *testing.T) {
				req := rejectedCompactionPipelineRequest(t, format)
				original := append([]byte(nil), req.Body...)
				ref, _, _, err := parseRemoteCompactionRequest(req.Body)
				require.NoError(t, err)
				apiKey := &ent.APIKey{ID: 301, ProjectID: 302}
				adapter := newRemoteCompactionAdapter(nil, nil, nil)
				adapter.summaries.SetDefault(remoteCompactionOwnerCacheKey(&PersistenceState{APIKey: apiKey}, remoteCompactionCacheKey(ref)), "verified retained source summary")
				executor := &responsesReasoningPipelineExecutor{
					failures: []error{rejectedNativeCompactionError()},
					events:   rejectedReasoningCompactionEvents(),
					response: &httpclient.Response{
						StatusCode: http.StatusOK,
						Headers:    http.Header{"Content-Type": {"application/json"}},
						Body:       []byte(`{"id":"cmp_next","object":"response.compaction","output":[{"type":"compaction","id":"cmp_next","encrypted_content":"next-native-checkpoint"}]}`),
					},
				}
				_, result, err := runRejectedReasoningPipeline(t, shared.WithSessionScope(t.Context(), "decryption-"+t.Name()), req, executor, "synthetic-credential", passThrough, 1,
					func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
						state.APIKey = apiKey
						return recoverRejectedRemoteCompaction(outbound, adapter, executor)
					})
				require.NoError(t, err)
				require.Len(t, executor.requests, 2)
				assertRejectedCompactionWindowPreserved(t, executor.requests[0].Body, executor.requests[1].Body)
				require.Equal(t, original, req.Body)
				if result.Stream {
					drainRejectedReasoningPipeline(t, result)
				}
			})
		}
	}
}

func TestResponsesRejectedCompactionDecryptionRestoresStoredSource(t *testing.T) {
	ctx, client, apiKey, adapter := rejectedCompactionStorage(t)
	seedRejectedCompactionSource(t, ctx, client, apiKey, requestexecution.StatusCompleted, "native-checkpoint-ciphertext")
	executor := &rejectedCompactionBridgeExecutor{rejection: rejectedNativeCompactionError()}
	req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
	_, result, err := runRejectedReasoningPipeline(t, ctx, req, executor, "synthetic-native-credential", true, 1,
		func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
			state.APIKey = apiKey
			return recoverRejectedRemoteCompaction(outbound, adapter, executor)
		})
	require.NoError(t, err)
	drainRejectedReasoningPipeline(t, result)
	require.Len(t, executor.continuations, 2)
	require.Len(t, executor.summaries, 1)
	assertRejectedCompactionWindowPreserved(t, executor.continuations[0], executor.continuations[1])
}

func TestResponsesRejectedCompactionDecryptionStopsWithoutSource(t *testing.T) {
	req := rejectedCompactionPipelineRequest(t, llm.APIFormatOpenAIResponse)
	original := append([]byte(nil), req.Body...)
	executor := &responsesReasoningPipelineExecutor{failures: []error{rejectedNativeCompactionError()}}
	_, result, err := runRejectedReasoningPipeline(t, shared.WithSessionScope(t.Context(), "missing-native-source"), req, executor, "synthetic-credential", true, 2,
		func(state *PersistenceState, outbound *PersistentOutboundTransformer) pipeline.Middleware {
			state.APIKey = &ent.APIKey{ID: 303, ProjectID: 304}
			return recoverRejectedRemoteCompaction(outbound, newRemoteCompactionAdapter(nil, nil, nil), executor)
		})
	require.Error(t, err)
	require.Nil(t, result)
	require.Len(t, executor.requests, 1, "missing history cannot become an empty summary or a blind retry")
	require.Equal(t, original, req.Body)
}
