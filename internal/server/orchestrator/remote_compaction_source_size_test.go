package orchestrator

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func largeNativeSourceFixture() []byte {
	// Synthetic opaque history has the same poor compression ratio as retained
	// encrypted reasoning. Repeated text alone would never hit the old limit.
	random := rand.NewChaCha8([32]byte{1, 2, 3})
	data := make([]byte, 4<<20)
	for i := 0; i < len(data); i += 8 {
		binary.LittleEndian.PutUint64(data[i:], random.Uint64())
	}
	return []byte(strings.Replace(nativeSourceFixture, "complete original result", base64.RawStdEncoding.EncodeToString(data), 1))
}

func TestResponsesRejectedNativeSourceLargeCheckpoint(t *testing.T) {
	body := largeNativeSourceFixture()
	encoded, err := json.Marshal(nativeCompactionSourceCheckpoint{Version: 1, Body: body})
	require.NoError(t, err)
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	require.NoError(t, err)
	packed := base64.RawStdEncoding.EncodeToString(encoder.EncodeAll(encoded, nil))
	require.NoError(t, encoder.Close())
	require.Greater(t, len(packed), localCompactionMaxSummaryBytes, "fixture must reproduce the production size mismatch")
	require.Less(t, len(body), nativeCompactionMaxSourceBytes)
	for _, mode := range []string{"http", "stream-item", "stream-completed", "compact"} {
		t.Run(mode, func(t *testing.T) {
			ctx, client, apiKey, adapter := rejectedCompactionStorage(t)
			wireBody := body
			format := llm.APIFormatOpenAIResponse
			if mode == "compact" {
				wireBody = []byte(strings.Replace(string(body), `,{"type":"compaction_trigger"}`, "", 1))
				format = llm.APIFormatOpenAIResponseCompact
			}
			state := &PersistenceState{APIKey: apiKey, RawProviderRequest: &httpclient.Request{Body: wireBody, APIFormat: string(format)}}
			middleware := retainNativeCompactionSources(&PersistentOutboundTransformer{state: state}, adapter)
			ref := &remoteCompactionReference{ID: "cmp_durable", EncryptedContent: "native-ciphertext"}
			if strings.HasPrefix(mode, "stream-") {
				data := `{"type":"response.output_item.done","item":{"type":"compaction","id":"cmp_durable","encrypted_content":"native-ciphertext"}}`
				if mode == "stream-completed" {
					data = `{"type":"response.completed","response":` + nativeSourceResponse + `}`
				}
				event := &httpclient.StreamEvent{Data: []byte(data)}
				stream, wrapErr := middleware.OnOutboundRawStream(ctx, streams.SliceStream([]*httpclient.StreamEvent{event}))
				require.NoError(t, wrapErr)
				t.Cleanup(func() { _ = stream.Close() })
				require.True(t, stream.Next(), "valid large source must be saved before publishing its checkpoint")
				require.Same(t, event, stream.Current())
				require.False(t, stream.Next())
				require.NoError(t, stream.Err())
			} else {
				response := &httpclient.Response{StatusCode: http.StatusOK, Body: []byte(nativeSourceResponse)}
				got, saveErr := middleware.OnOutboundRawResponse(ctx, response)
				require.NoError(t, saveErr)
				require.Same(t, response, got)
			}
			stored, loadErr := adapter.systemService.LoadUnifiedHistory(ctx, []string{historyCheckpointAlias(ref)})
			require.NoError(t, loadErr)
			require.NotEmpty(t, stored[historyCheckpointAlias(ref)])
			require.NotContains(t, stored[historyCheckpointAlias(ref)], "complete original")

			// Reload from durable storage, with no prior request service or cache.
			restarted := newRemoteCompactionAdapter(nil, nil, biz.NewSystemService(biz.SystemServiceParams{Ent: client}))
			source, restoreErr := restarted.loadNativeCompactionSource(ctx, ref, state)
			require.NoError(t, restoreErr)
			require.NotNil(t, source)
			if mode == "compact" {
				require.JSONEq(t, string(body), string(source.body))
			} else {
				require.Equal(t, body, source.body, "full source must survive without truncation")
			}
		})
	}
}

func TestResponsesRejectedNativeSourceLegacyEncoding(t *testing.T) {
	ctx, _, apiKey, adapter := rejectedCompactionStorage(t)
	state := &PersistenceState{APIKey: apiKey}
	ref := &remoteCompactionReference{ID: "cmp_legacy_source", EncryptedContent: "opaque-original"}
	encoded, err := json.Marshal(nativeCompactionSourceCheckpoint{Version: 1, Body: []byte(nativeSourceFixture)})
	require.NoError(t, err)
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	require.NoError(t, err)
	packed := base64.RawStdEncoding.EncodeToString(encoder.EncodeAll(encoded, nil))
	require.NoError(t, encoder.Close())
	aead, err := adapter.localCompactionCipher(ctx)
	require.NoError(t, err)
	aad, err := nativeCompactionSourceAAD(ref, state)
	require.NoError(t, err)
	// Recreate the old writer, not the new source serialization helper.
	sealed, err := sealLocalCompactionSummary(aead, aad, packed)
	require.NoError(t, err)
	require.NoError(t, adapter.systemService.SaveNativeCompactionSource(ctx, apiKey.ProjectID, apiKey.ID, remoteCompactionCacheKey(ref), sealed))
	source, err := adapter.loadNativeCompactionSource(ctx, ref, state)
	require.NoError(t, err)
	require.Equal(t, nativeSourceFixture, string(source.body))

	for _, other := range []*ent.APIKey{{ID: apiKey.ID + 1, ProjectID: apiKey.ProjectID}, {ID: apiKey.ID, ProjectID: apiKey.ProjectID + 1}} {
		require.NoError(t, adapter.systemService.SaveNativeCompactionSource(ctx, other.ProjectID, other.ID, remoteCompactionCacheKey(ref), sealed))
		otherCtx := contexts.WithProjectID(contexts.WithAPIKey(ctx, other), other.ProjectID)
		_, err = adapter.loadNativeCompactionSource(otherCtx, ref, &PersistenceState{APIKey: other})
		require.Error(t, err, "a copied database record must still authenticate its owner")
	}
}

func TestResponsesRejectedNativeSourceSizeGuard(t *testing.T) {
	ctx, _, apiKey, adapter := rejectedCompactionStorage(t)
	ref := &remoteCompactionReference{ID: "cmp_oversized", EncryptedContent: "opaque-original"}
	state := &PersistenceState{APIKey: apiKey}
	err := adapter.retainNativeCompactionSource(ctx, ref, state, &remoteCompactionSource{body: make([]byte, nativeCompactionMaxSourceBytes+1)})
	require.ErrorContains(t, err, "source exceeds limit")
	sealed, err := adapter.systemService.LoadNativeCompactionSource(ctx, apiKey.ProjectID, apiKey.ID, remoteCompactionCacheKey(ref))
	require.NoError(t, err)
	require.Empty(t, sealed)
}

func TestSealedLocalCompactionSizeLimitUnchanged(t *testing.T) {
	aead, err := newLocalCompactionCipher("synthetic")
	require.NoError(t, err)
	aad := []byte("synthetic-owner")
	text := strings.Repeat("x", localCompactionMaxSummaryBytes)
	sealed, err := sealLocalCompactionSummary(aead, aad, text)
	require.NoError(t, err)
	got, err := openLocalCompactionSummary(aead, aad, sealed)
	require.NoError(t, err)
	require.Equal(t, text, got)
	_, err = sealLocalCompactionSummary(aead, aad, text+"x")
	require.Error(t, err)
	sealed, err = sealCompactionText(aead, aad, text+"x", nativeCompactionMaxSealedTextBytes())
	require.NoError(t, err)
	_, err = openLocalCompactionSummary(aead, aad, sealed)
	require.Error(t, err)
	_, err = openCompactionText(aead, []byte("other-owner"), sealed, nativeCompactionMaxSealedTextBytes())
	require.Error(t, err)
}
