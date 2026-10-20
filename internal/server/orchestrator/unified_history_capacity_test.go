package orchestrator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestUnifiedHistoryInputAllowanceKeepsOutputAndConfiguredWindows(t *testing.T) {
	_, _, state, _ := unifiedHistoryFixture(t)
	candidate := state.CurrentCandidate
	contextTokens := biz.BundledModelContextLimit("gpt-6-astra")
	target, ceiling := unifiedHistoryInputBudgets([]byte(`{"model":"gpt-6-astra"}`), candidate, 0, contextTokens)
	require.Equal(t, contextTokens-contextTokens/4, target)
	require.Greater(t, ceiling, target)
	require.Less(t, ceiling, biz.BundledModelHistoryInputLimit("gpt-6-astra", contextTokens), "leave room for framing too")
	smallTarget, smallCeiling := unifiedHistoryInputBudgets([]byte(`{}`), candidate, 0, 372000)
	require.Equal(t, 279000, smallTarget)
	require.Equal(t, smallTarget, smallCeiling, "a different configured window cannot inherit the catalog contract")
	candidate.Models[0].ActualModel = "tenant/gpt-6-astra"
	unknownTarget, unknownCeiling := unifiedHistoryInputBudgets([]byte(`{}`), candidate, 0, contextTokens)
	require.Equal(t, target, unknownTarget)
	require.Equal(t, unknownTarget, unknownCeiling)
	candidate.Models[0].ActualModel = "gpt-6-astra"
	overriddenTarget, overriddenCeiling := unifiedHistoryInputBudgets([]byte(`{"model":"another-destination"}`), candidate, 0, contextTokens)
	require.Equal(t, target, overriddenTarget)
	require.Equal(t, overriddenTarget, overriddenCeiling, "a body override cannot borrow another model's larger input limit")
	_, reserved := unifiedHistoryInputBudgets([]byte(`{"max_output_tokens":300000}`), candidate, 0, contextTokens)
	require.LessOrEqual(t, reserved, contextTokens-300000-4096)
	noInput, noCeiling := unifiedHistoryInputBudgets([]byte(`{"max_output_tokens":2000000}`), candidate, 0, contextTokens)
	require.Zero(t, noInput)
	require.Zero(t, noCeiling)
}

func TestUnifiedHistoryCachedNotesCanExceedTargetWithoutResummarization(t *testing.T) {
	for _, tooLarge := range []bool{false, true} {
		t.Run(fmt.Sprint(tooLarge), func(t *testing.T) {
			ctx, _, state, adapter := unifiedHistoryFixture(t)
			state.CurrentCandidate.ContextTokens = 0
			var input []json.RawMessage
			for i := range 10 {
				user, err := json.Marshal(map[string]string{"role": "user", "content": fmt.Sprintf("constraint_%d: ", i) + historyTestEntropy(50000, uint64(i))})
				require.NoError(t, err)
				assistant, err := json.Marshal(map[string]string{"role": "assistant", "content": fmt.Sprintf("original evidence_%d: ", i) + historyTestEntropy(75000, uint64(i+100))})
				require.NoError(t, err)
				input = append(input, user, assistant)
			}
			input = append(input, json.RawMessage(`{"role":"user","content":"Continue."}`))
			body, err := json.Marshal(map[string]any{"model": "gpt-6-astra", "reasoning": map[string]string{"effort": "xhigh"}, "input": input})
			require.NoError(t, err)
			envelope, items, err := decodeResponsesInput(body)
			require.NoError(t, err)
			target, ceiling := unifiedHistoryInputBudgets(body, state.CurrentCandidate, 0, biz.BundledModelContextLimit("gpt-6-astra"))
			plan, supported, err := planUnifiedHistory(envelope, items, state, state.CurrentCandidate, target)
			require.NoError(t, err)
			require.True(t, supported)
			require.Len(t, plan.segments, 9)
			stored := make(map[string]string)
			// Size cached notes in tokens, independently of source transport bytes.
			noteTokens := (target-plan.mandatory)/(len(plan.segments)-1) + 1024
			if tooLarge {
				noteTokens = (ceiling-plan.mandatory)/(len(plan.segments)-1) + 1024
			}
			for i := range len(plan.segments) - 1 {
				segment := &plan.segments[i]
				segment.summary = fmt.Sprintf("immutable note_%d: ", i) + strings.Repeat("s ", noteTokens*4/5)
				stored[segment.key], err = adapter.sealHistoryRecord(ctx, segment.key, []byte(segment.summary))
				require.NoError(t, err)
			}
			require.NoError(t, adapter.systemService.SaveUnifiedHistory(ctx, stored))
			require.Error(t, validateUnifiedHistoryMinimum(plan, target), "reproduce the old post-generation budget failure")
			executor := &unifiedConcurrentExecutor{}
			compiled, ok, err := adapter.compileUnifiedContext(ctx, body, state, executor)
			if tooLarge {
				require.False(t, ok)
				var limit *llm.ResponseError
				require.ErrorAs(t, err, &limit)
				require.Equal(t, "unified_history_budget_exceeded", limit.Detail.Code)
				require.Equal(t, http.StatusBadRequest, responses.NewInboundTransformer().TransformError(ctx, err).StatusCode)
				require.Empty(t, executor.summaries, "cached material already exceeds the hard limit; do not pay for missing siblings")
				return
			}
			require.NoError(t, err)
			require.True(t, ok)
			require.Greater(t, unifiedContextSize(compiled), target)
			require.LessOrEqual(t, unifiedContextSize(compiled), ceiling)
			require.Len(t, executor.summaries, 1)
			require.Equal(t, "xhigh", gjson.GetBytes(compiled, "reasoning.effort").String())
			for _, segment := range plan.segments[:len(plan.segments)-1] {
				require.Contains(t, string(compiled), segment.summary)
			}
			for _, item := range input {
				if gjson.GetBytes(item, "role").String() == "user" {
					require.Contains(t, string(compiled), gjson.GetBytes(item, "content").String())
				}
			}
			// Restart and resend must reuse all completed notes byte-for-byte.
			adapter = newRemoteCompactionAdapter(adapter.requestService, nil, adapter.systemService)
			again, ok, err := adapter.compileUnifiedContext(ctx, body, state, executor)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, compiled, again)
			require.Len(t, executor.summaries, 1)
		})
	}
}

func TestUnifiedHistoryFinalSizeLimitIsNotRetryableServerFailure(t *testing.T) {
	plan := &unifiedHistoryPlan{segments: []unifiedHistorySegment{{key: "summary:synthetic", summary: strings.Repeat("long note ", 1000)}}}
	_, err := renderUnifiedHistory(map[string]json.RawMessage{"model": json.RawMessage(`"synthetic"`)}, plan, 1024)
	require.Error(t, err)
	transformed := responses.NewInboundTransformer().TransformError(t.Context(), &remoteCompactionPreparationError{cause: err})
	require.Equal(t, http.StatusBadRequest, transformed.StatusCode)
	require.Equal(t, "unified_history_budget_exceeded", gjson.GetBytes(transformed.Body, "error.code").String())
}
