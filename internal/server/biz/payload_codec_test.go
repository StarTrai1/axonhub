package biz

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStoredPayloadCompressionRoundTrip(t *testing.T) {
	t.Parallel()

	raw := []byte("{\"input\":\"" + strings.Repeat("stable conversation context ", 8192) + "\"}")

	compressed, err := CompressStoredPayload(raw)
	require.NoError(t, err)
	require.True(t, isCompressedStoredPayload(compressed))
	require.Less(t, len(compressed), len(raw))
	require.True(t, json.Valid(compressed))

	decoded, err := DecodeStoredPayload(compressed)
	require.NoError(t, err)
	require.Equal(t, raw, decoded)
}

func TestStoredPayloadCompressionLeavesSmallPayloadUnchanged(t *testing.T) {
	t.Parallel()

	raw := []byte("{\"input\":\"hello\"}")

	compressed, err := CompressStoredPayload(raw)
	require.NoError(t, err)
	require.Equal(t, raw, []byte(compressed))

	decoded, err := DecodeStoredPayload(compressed)
	require.NoError(t, err)
	require.Equal(t, raw, decoded)
}

func TestStoredPayloadCompressionIsIdempotent(t *testing.T) {
	t.Parallel()

	raw := []byte("{\"input\":\"" + strings.Repeat("repeated context ", 8192) + "\"}")

	compressed, err := CompressStoredPayload(raw)
	require.NoError(t, err)
	recompressed, err := CompressStoredPayload(compressed)
	require.NoError(t, err)
	require.Equal(t, compressed, recompressed)
}

func TestStoredPayloadCompressionRejectsChecksumMismatch(t *testing.T) {
	t.Parallel()

	raw := []byte("{\"input\":\"" + strings.Repeat("repeated context ", 8192) + "\"}")
	compressed, err := CompressStoredPayload(raw)
	require.NoError(t, err)

	var envelope databasePayloadEnvelope
	require.NoError(t, json.Unmarshal(compressed, &envelope))
	envelope.SHA256 = strings.Repeat("0", 64)
	corrupted, err := json.Marshal(envelope)
	require.NoError(t, err)

	_, err = DecodeStoredPayload(corrupted)
	require.ErrorContains(t, err, "checksum mismatch")
}

func TestStoredRequestBodyReferenceMatchesCompressedParent(t *testing.T) {
	t.Parallel()

	raw := []byte("{\"input\":\"" + strings.Repeat("shared request history ", 8192) + "\"}")
	parent, err := CompressStoredPayload(raw)
	require.NoError(t, err)

	reference, referenced, err := referenceStoredRequestBody(42, parent, raw)
	require.NoError(t, err)
	require.True(t, referenced)
	require.Less(t, len(reference), len(parent))
	require.True(t, json.Valid(reference))

	decoded, ok, err := decodeStoredRequestBodyReference(reference)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 42, decoded.RequestID)
	require.NoError(t, validateStoredRequestBodyReference(decoded, raw))
}

func TestStoredRequestBodyReferenceKeepsDifferentBody(t *testing.T) {
	t.Parallel()

	parentRaw := []byte("{\"input\":\"" + strings.Repeat("parent ", 16384) + "\"}")
	candidate := []byte("{\"input\":\"" + strings.Repeat("candidate ", 16384) + "\"}")
	parent, err := CompressStoredPayload(parentRaw)
	require.NoError(t, err)

	stored, referenced, err := referenceStoredRequestBody(42, parent, candidate)
	require.NoError(t, err)
	require.False(t, referenced)
	require.Equal(t, candidate, []byte(stored))
}
