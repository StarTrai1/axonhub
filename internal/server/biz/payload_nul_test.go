package biz

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestStoredPayloadPreservesNul(t *testing.T) {
	for _, tc := range []struct {
		body    string
		encoded bool
	}{
		{`{"text":"hello\u0000world","call_id":"call_1"}`, true},
		{`{"\u0000":"value","tool":{"output":"\u0000"}}`, true},
		{`{"text":"\\\u0000"}`, true},
		{`{"text":"\\u0000"}`, false},
		{`{"text":"\\\\u0000"}`, false},
		{`{"text":"ordinary"}`, false},
	} {
		t.Run(tc.body, func(t *testing.T) {
			raw := []byte(tc.body)
			stored, err := CompressStoredPayload(raw)
			require.NoError(t, err)
			require.Equal(t, tc.encoded, isCompressedStoredPayload(stored))
			decoded, err := DecodeStoredPayload(stored)
			require.NoError(t, err)
			require.Equal(t, raw, decoded)
			require.Equal(t, tc.body, string(raw), "storage must not mutate outbound bytes")
			again, err := CompressStoredPayload(stored)
			require.NoError(t, err)
			require.Equal(t, stored, again)
		})
	}
}

func TestStoredPayloadNul_Postgres(t *testing.T) {
	dsn := os.Getenv("AXONHUB_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("AXONHUB_TEST_PG_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	raw := []byte(`{"input":[{"type":"function_call_output","call_id":"call_1","output":"left\u0000right"}],"prompt_cache_key":"stable"}`)
	var result string
	err = db.QueryRowContext(t.Context(), "SELECT $1::jsonb::text", string(raw)).Scan(&result)
	require.Error(t, err, "the original NUL escape cannot be stored as PostgreSQL jsonb")
	stored, err := CompressStoredPayload(raw)
	require.NoError(t, err)
	err = db.QueryRowContext(t.Context(), "SELECT $1::jsonb::text", string(stored)).Scan(&result)
	require.NoError(t, err)
	// jsonb may reorder envelope fields. Readers must recognize the persisted
	// representation, not just the serializer's original key order.
	decoded, err := DecodeStoredPayload([]byte(result))
	require.NoError(t, err)
	require.Equal(t, raw, decoded)

	// Duplicate execution bodies can reference a parent envelope returned by
	// jsonb. Both envelope kinds must survive its normalization.
	raw = []byte(`{"input":"` + strings.Repeat(`stable\u0000prefix `, 5000) + `"}`)
	stored, err = CompressStoredPayload(raw)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT $1::jsonb::text", string(stored)).Scan(&result))
	reference, ok, err := referenceStoredRequestBody(17, []byte(result), raw)
	require.NoError(t, err)
	require.True(t, ok)
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT $1::jsonb::text", string(reference)).Scan(&result))
	parsed, ok, err := decodeStoredRequestBodyReference([]byte(result))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 17, parsed.RequestID)
	require.NoError(t, validateStoredRequestBodyReference(parsed, raw))
}
