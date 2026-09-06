package orchestrator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm/transformer/shared"
)

var responsesSessionCloneBenchmarkResult []json.RawMessage

func TestResponsesSessionBulkCloneDoesNotAliasAdjacentItems(t *testing.T) {
	values := []json.RawMessage{[]byte(`"first"`), nil, []byte(`"second"`)}
	cloned := cloneResponseSessionValues(values)
	require.Equal(t, values, cloned)
	cloned[0][1] = 'F'
	cloned[0] = append(cloned[0], 'x')
	require.Equal(t, json.RawMessage(`"first"`), values[0])
	require.Equal(t, json.RawMessage(`"second"`), cloned[2])
	require.Nil(t, cloned[1])
}

func TestResponsesSessionReplacementMaintainsEvictionOrderAndBytes(t *testing.T) {
	store := newResponsesSessionStore()
	first := responsesSessionKey{scope: "scope", responseID: "first"}
	second := responsesSessionKey{scope: "scope", responseID: "second"}
	store.insertLocked(first, &responsesSessionRecord{updatedAt: time.Now(), size: 10})
	store.insertLocked(second, &responsesSessionRecord{updatedAt: time.Now(), size: 20})
	store.insertLocked(first, &responsesSessionRecord{updatedAt: time.Now(), size: 30})
	require.Equal(t, 50, store.totalBytes)
	require.Equal(t, 2, store.order.Len())
	require.True(t, store.evictOldestLocked())
	require.Nil(t, store.byResponse[second])
	require.NotNil(t, store.byResponse[first])
	require.Equal(t, 30, store.totalBytes)
	require.Len(t, store.positions, 1)
}

func BenchmarkResponsesSessionClone(b *testing.B) {
	values := make([]json.RawMessage, 256)
	for index := range values {
		values[index] = bytes.Repeat([]byte("x"), 2048)
	}
	for _, variant := range []struct {
		name  string
		clone func([]json.RawMessage) []json.RawMessage
	}{
		{name: "per_item_baseline", clone: func(input []json.RawMessage) []json.RawMessage {
			result := make([]json.RawMessage, 0, len(input))
			for _, item := range input {
				result = append(result, append(json.RawMessage(nil), item...))
			}
			return result
		}},
		{name: "bulk", clone: cloneResponseSessionValues},
	} {
		b.Run(variant.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(rawResponseSessionValuesSize(values)))
			for b.Loop() {
				responsesSessionCloneBenchmarkResult = variant.clone(values)
			}
		})
	}
}

func BenchmarkResponsesSessionLookup(b *testing.B) {
	for _, count := range []int{16, 256, responsesSessionMaxRecords} {
		b.Run(fmt.Sprintf("records_%d", count), func(b *testing.B) {
			store := newResponsesSessionStore()
			ctx := shared.WithSessionScope(b.Context(), "scope")
			for index := range count {
				store.insertLocked(responsesSessionKey{scope: "scope", responseID: fmt.Sprint(index)}, &responsesSessionRecord{
					input: []json.RawMessage{[]byte(`{"content":"request"}`)}, updatedAt: time.Now(), size: 21,
				})
			}
			for _, variant := range []string{"scan_locked_baseline", "ordered_snapshot"} {
				b.Run(variant, func(b *testing.B) {
					b.ReportAllocs()
					b.RunParallel(func(worker *testing.PB) {
						for worker.Next() {
							var snapshot *responsesSessionRecord
							if variant == "scan_locked_baseline" {
								store.mu.Lock()
								now := time.Now()
								for key, record := range store.byResponse {
									if now.Sub(record.updatedAt) > responsesSessionTTL {
										store.removeLocked(key)
									}
								}
								record := store.byResponse[responsesSessionKey{scope: "scope", responseID: "0"}]
								if record != nil {
									snapshot = &responsesSessionRecord{
										input: cloneResponseSessionValues(record.input), output: cloneResponseSessionValues(record.output),
										sessionID: record.sessionID, updatedAt: record.updatedAt, size: record.size,
									}
								}
								store.mu.Unlock()
							} else {
								snapshot = store.lookup(ctx, "0")
							}
							if snapshot == nil {
								b.Error("missing snapshot")
							}
						}
					})
				})
			}
		})
	}
}
