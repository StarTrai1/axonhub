package orchestrator

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"golang.org/x/sync/errgroup"
)

func TestUnifiedHistoryResumedSmallBatchRemainsConcurrent(t *testing.T) {
	for _, missing := range []int{2, 4} {
		t.Run(fmt.Sprint(missing), func(t *testing.T) {
			ctx, _, state, adapter := unifiedHistoryFixture(t)
			body := unifiedLongToolHistory(t, 24)
			envelope, items, err := decodeResponsesInput(body)
			require.NoError(t, err)
			plan, supported, err := planUnifiedHistory(envelope, items, state, state.CurrentCandidate, 279000)
			require.NoError(t, err)
			require.True(t, supported)
			require.Greater(t, len(plan.segments), missing)
			stored := make(map[string]string)
			for _, segment := range plan.segments[missing:] {
				sealed, err := adapter.sealHistoryRecord(ctx, segment.key, []byte("previously completed original segment"))
				require.NoError(t, err)
				stored[segment.key] = sealed
			}
			require.NoError(t, adapter.systemService.SaveUnifiedHistory(ctx, stored))
			started, release := make(chan struct{}, missing), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			executor := &unifiedConcurrentExecutor{started: started, release: release}
			var group errgroup.Group
			var count int
			group.Go(func() (err error) {
				defer func() {
					if value := recover(); value != nil {
						err = fmt.Errorf("preparation panic: %v", value)
					}
				}()
				count, supported, err = adapter.hydrateUnifiedSegments(ctx, plan.segments, state, executor, nil)
				return err
			})
			t.Cleanup(func() { unblock(); _ = group.Wait() })
			for range missing {
				select {
				case <-started:
				case <-time.After(10 * time.Second):
					t.Fatal("remaining independent segments started serially")
				}
			}
			unblock()
			require.NoError(t, group.Wait())
			require.True(t, supported)
			require.Equal(t, missing, count)
			require.Equal(t, int32(missing), executor.max.Load())
			require.Len(t, executor.summaries, missing, "completed segments must not regenerate")
		})
	}
}

func TestUnifiedHistoryInstructionOnlySegmentStaysExactWithoutGeneration(t *testing.T) {
	ctx, _, state, adapter := unifiedHistoryFixture(t)
	body := unifiedLongToolHistory(t, 12)
	envelope, items, err := decodeResponsesInput(body)
	require.NoError(t, err)
	var prefix []json.RawMessage
	for i := range 2 {
		item, err := json.Marshal(map[string]string{"role": "developer", "content": fmt.Sprintf("original instruction %d: ", i) + strings.Repeat("x", 64000)})
		require.NoError(t, err)
		prefix = append(prefix, item)
	}
	items = append(prefix, items...)
	plan, supported, err := planUnifiedHistory(envelope, items, state, state.CurrentCandidate, 600000)
	require.NoError(t, err)
	require.True(t, supported)
	require.Empty(t, plan.segments[0].key)
	executor := &unifiedConcurrentExecutor{}
	count, supported, err := adapter.hydrateUnifiedSegments(ctx, plan.segments, state, executor, nil)
	require.NoError(t, err)
	require.True(t, supported)
	require.Equal(t, len(plan.segments)-1, count)
	for _, request := range executor.summaries {
		require.NotContains(t, string(request), "original instruction 0:")
	}
	compiled, err := renderUnifiedHistory(envelope, plan, 600000)
	require.NoError(t, err)
	for i, item := range prefix {
		require.JSONEq(t, string(item), gjson.GetBytes(compiled, fmt.Sprintf("input.%d", i)).Raw)
	}
	require.Len(t, gjson.GetBytes(compiled, `input.#(role=="assistant")#`).Array(), count)
}

func TestUnifiedHistoryReplyRetrievalUsesAnswerInsteadOfTransportIdentifiers(t *testing.T) {
	text := `<send_user_message_question_reply>
[{"questionItemId":"request_user_input_async call_metadata","question":"Choose the resource lifecycle","answer":"Use deinit after disconnect"}]
</send_user_message_question_reply>`
	item, err := json.Marshal(map[string]string{"role": "user", "content": text})
	require.NoError(t, err)
	query := unifiedHistoryQuery(item)
	require.Equal(t, "choose the resource lifecycle\nuse deinit after disconnect", query)
	unrelated := []json.RawMessage{json.RawMessage(`{"role":"assistant","content":"request_user_input_async call_metadata questionItemId"}`)}
	unrelatedSegments := []unifiedHistorySegment{{items: unrelated}}
	require.Equal(t, []bool{true}, unifiedHistorySegmentRelevance(strings.ToLower(text), unrelatedSegments), "old query incorrectly recalls transport documentation")
	require.Equal(t, []bool{false}, unifiedHistorySegmentRelevance(query, unrelatedSegments))
	require.Equal(t, []bool{true}, unifiedHistorySegmentRelevance(query, []unifiedHistorySegment{{items: []json.RawMessage{json.RawMessage(`{"role":"assistant","content":"exact deinit evidence"}`)}}}))
	require.Equal(t, text, unifiedMessageText(item), "retrieval normalization must not alter the original user reply")
	require.Equal(t, "plain original query", unifiedHistoryQuery(json.RawMessage(`{"role":"user","content":"Plain original query"}`)))
}

func TestUnifiedHistoryRetrievalIgnoresUbiquitousNamesAndFindsMixedScriptIdentifiers(t *testing.T) {
	query := "AX7018F：做好资源准备init和清理deinit，查看audio_cleanup原文"
	segments := []unifiedHistorySegment{
		{items: []json.RawMessage{json.RawMessage(`{"role":"assistant","content":"AX7018F old power measurements"}`)}},
		{items: []json.RawMessage{json.RawMessage(`{"role":"assistant","content":"AX7018F old advertising measurements"}`)}},
		{items: []json.RawMessage{json.RawMessage(`{"role":"assistant","content":"AX7018F exact audio_cleanup and deinit code"}`)}},
		{items: []json.RawMessage{json.RawMessage(`{"role":"assistant","content":"AX7018F old transport traces"}`)}},
	}
	require.Contains(t, unifiedHistoryTerms(query), "deinit")
	require.Contains(t, unifiedHistoryTerms(query), "audio_cleanup")
	require.Equal(t, []bool{false, false, true, false}, unifiedHistorySegmentRelevance(query, segments))
}
