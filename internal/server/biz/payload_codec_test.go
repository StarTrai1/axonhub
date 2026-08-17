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
