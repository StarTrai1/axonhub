package biz

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/klauspost/compress/zstd"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/objects"
)

const (
	databasePayloadCodecMarker          = "axonhub.payload"
	databasePayloadCodecVersion         = 1
	databasePayloadCompressionThreshold = 64 * 1024
	databaseRequestBodyReferenceMarker  = "axonhub.request_body_ref"
	databaseRequestBodyReferenceVersion = 1
)

type databasePayloadEnvelope struct {
	Marker     string `json:"_axonhub_payload"`
	Codec      string `json:"codec"`
	Version    int    `json:"version"`
	RawBytes   int    `json:"raw_bytes"`
	SHA256     string `json:"sha256"`
	Compressed string `json:"data"`
}

type databaseRequestBodyReferenceEnvelope struct {
	Marker    string `json:"_axonhub_payload"`
	Version   int    `json:"version"`
	RequestID int    `json:"request_id"`
	RawBytes  int    `json:"raw_bytes"`
	SHA256    string `json:"sha256"`
}

var (
	databasePayloadEnvelopePrefix      = []byte(`{"_axonhub_payload":"axonhub.payload"`)
	databaseRequestBodyReferencePrefix = []byte(`{"_axonhub_payload":"axonhub.request_body_ref"`)
	databasePayloadEncoderPool         sync.Pool
	databasePayloadDecoderPool         sync.Pool
)

// CompressStoredPayload losslessly encodes large or JSON-NUL-containing payloads for database persistence.
func CompressStoredPayload(raw []byte) (objects.JSONRawMessage, error) {
	needsEnvelope := hasJSONNulEscape(raw)
	if (len(raw) < databasePayloadCompressionThreshold && !needsEnvelope) || isCompressedStoredPayload(raw) {
		return objects.JSONRawMessage(raw), nil
	}

	encoder := databasePayloadEncoderPool.Get()
	if encoder == nil {
		created, err := zstd.NewWriter(nil,
			zstd.WithEncoderLevel(zstd.SpeedFastest),
			zstd.WithEncoderConcurrency(1),
		)
		if err != nil {
			return nil, fmt.Errorf("create payload compressor: %w", err)
		}
		encoder = created
	}
	zstdEncoder, ok := encoder.(*zstd.Encoder)
	if !ok {
		return nil, errors.New("invalid payload compressor in pool")
	}
	compressed := zstdEncoder.EncodeAll(raw, nil)
	databasePayloadEncoderPool.Put(zstdEncoder)

	if !needsEnvelope && base64.RawStdEncoding.EncodedLen(len(compressed)) >= len(raw) {
		return objects.JSONRawMessage(raw), nil
	}
	encoded := base64.RawStdEncoding.EncodeToString(compressed)

	digest := sha256.Sum256(raw)
	envelope, err := json.Marshal(databasePayloadEnvelope{
		Marker:     databasePayloadCodecMarker,
		Codec:      "zstd",
		Version:    databasePayloadCodecVersion,
		RawBytes:   len(raw),
		SHA256:     hex.EncodeToString(digest[:]),
		Compressed: encoded,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal compressed payload envelope: %w", err)
	}
	if !needsEnvelope && len(envelope) >= len(raw) {
		return objects.JSONRawMessage(raw), nil
	}

	return objects.JSONRawMessage(envelope), nil
}

// PostgreSQL jsonb rejects actual U+0000 escapes. Encode the complete original
// bytes instead of deleting text, so tool payloads and replay/cache prefixes survive.
// Escaped backslashes followed by u0000 are ordinary text and need no envelope.
func hasJSONNulEscape(raw []byte) bool {
	if !bytes.Contains(raw, []byte(`\u0000`)) {
		return false
	}
	for i := 0; i+1 < len(raw); i++ {
		if raw[i] == '\\' {
			if bytes.HasPrefix(raw[i:], []byte(`\u0000`)) {
				return true
			}
			i++
		}
	}
	return false
}

// DecodeStoredPayload transparently decodes payloads compressed by CompressStoredPayload.
func DecodeStoredPayload(raw []byte) ([]byte, error) {
	if !isCompressedStoredPayload(raw) {
		return raw, nil
	}

	var envelope databasePayloadEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode compressed payload envelope: %w", err)
	}
	if envelope.Codec != "zstd" || envelope.Version != databasePayloadCodecVersion {
		return nil, errors.New("unsupported compressed payload codec or version")
	}
	if envelope.RawBytes < 0 {
		return nil, errors.New("invalid compressed payload length")
	}
	compressed, err := base64.RawStdEncoding.DecodeString(envelope.Compressed)
	if err != nil {
		return nil, fmt.Errorf("decode compressed payload data: %w", err)
	}
	decoder := databasePayloadDecoderPool.Get()
	if decoder == nil {
		created, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
		if err != nil {
			return nil, fmt.Errorf("create payload decompressor: %w", err)
		}
		decoder = created
	}
	zstdDecoder, ok := decoder.(*zstd.Decoder)
	if !ok {
		return nil, errors.New("invalid payload decompressor in pool")
	}
	decompressed, err := zstdDecoder.DecodeAll(compressed, nil)
	databasePayloadDecoderPool.Put(zstdDecoder)
	if err != nil {
		return nil, fmt.Errorf("decompress stored payload: %w", err)
	}
	if len(decompressed) != envelope.RawBytes {
		return nil, fmt.Errorf("decompressed payload length mismatch: got %d, want %d", len(decompressed), envelope.RawBytes)
	}
	digest := sha256.Sum256(decompressed)
	if hex.EncodeToString(digest[:]) != envelope.SHA256 {
		return nil, errors.New("decompressed payload checksum mismatch")
	}

	return decompressed, nil
}

func referenceStoredRequestBody(
	parentRequestID int,
	parentStored []byte,
	candidateRaw []byte,
) (objects.JSONRawMessage, bool, error) {
	if parentRequestID <= 0 || len(candidateRaw) < databasePayloadCompressionThreshold {
		return objects.JSONRawMessage(candidateRaw), false, nil
	}

	parentBytes, parentSHA256, err := storedPayloadIdentity(parentStored)
	if err != nil {
		return nil, false, fmt.Errorf("identify parent request body: %w", err)
	}

	var candidateBytes int
	var candidateSHA256 string
	if isCompressedStoredPayload(parentStored) {
		candidateBytes, candidateSHA256 = rawPayloadIdentity(candidateRaw)
	} else {
		candidateBytes, candidateSHA256, err = storedPayloadIdentity(candidateRaw)
		if err != nil {
			return nil, false, fmt.Errorf("identify candidate request body: %w", err)
		}
	}
	if parentBytes != candidateBytes {
		return objects.JSONRawMessage(candidateRaw), false, nil
	}
	if parentSHA256 != candidateSHA256 {
		return objects.JSONRawMessage(candidateRaw), false, nil
	}

	reference, err := json.Marshal(databaseRequestBodyReferenceEnvelope{
		Marker:    databaseRequestBodyReferenceMarker,
		Version:   databaseRequestBodyReferenceVersion,
		RequestID: parentRequestID,
		RawBytes:  candidateBytes,
		SHA256:    candidateSHA256,
	})
	if err != nil {
		return nil, false, fmt.Errorf("marshal request body reference: %w", err)
	}

	return objects.JSONRawMessage(reference), true, nil
}

func storedPayloadIdentity(raw []byte) (int, string, error) {
	if !isCompressedStoredPayload(raw) {
		stored, err := json.Marshal(objects.JSONRawMessage(raw))
		if err != nil {
			return 0, "", fmt.Errorf("encode database JSON payload identity: %w", err)
		}

		length, digest := rawPayloadIdentity(stored)
		return length, digest, nil
	}

	// Identity checks run before execution persistence. Ignore the base64 data
	// field so a multi-MiB compressed body is not copied just to read its digest.
	var envelope struct {
		Codec    string `json:"codec"`
		Version  int    `json:"version"`
		RawBytes int    `json:"raw_bytes"`
		SHA256   string `json:"sha256"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return 0, "", fmt.Errorf("decode compressed payload identity: %w", err)
	}
	if envelope.Codec != "zstd" || envelope.Version != databasePayloadCodecVersion {
		return 0, "", errors.New("unsupported compressed payload identity")
	}
	if envelope.RawBytes < 0 || !isSHA256Hex(envelope.SHA256) {
		return 0, "", errors.New("invalid compressed payload identity")
	}

	return envelope.RawBytes, envelope.SHA256, nil
}

func rawPayloadIdentity(raw []byte) (int, string) {
	digest := sha256.Sum256(raw)
	return len(raw), hex.EncodeToString(digest[:])
}

func decodeStoredRequestBodyReference(raw []byte) (databaseRequestBodyReferenceEnvelope, bool, error) {
	if !storedPayloadHasMarker(raw, databaseRequestBodyReferencePrefix, databaseRequestBodyReferenceMarker) {
		return databaseRequestBodyReferenceEnvelope{}, false, nil
	}

	var reference databaseRequestBodyReferenceEnvelope
	if err := json.Unmarshal(raw, &reference); err != nil {
		return databaseRequestBodyReferenceEnvelope{}, true, fmt.Errorf("decode request body reference: %w", err)
	}
	if reference.Marker != databaseRequestBodyReferenceMarker ||
		reference.Version != databaseRequestBodyReferenceVersion ||
		reference.RequestID <= 0 ||
		reference.RawBytes < 0 ||
		!isSHA256Hex(reference.SHA256) {
		return databaseRequestBodyReferenceEnvelope{}, true, errors.New("invalid request body reference")
	}

	return reference, true, nil
}

func validateStoredRequestBodyReference(reference databaseRequestBodyReferenceEnvelope, raw []byte) error {
	if requestBodyReferenceMatches(reference, raw) {
		return nil
	}

	// References created before database JSON normalization was accounted for
	// identify the client bytes, while Ent stores HTML-sensitive characters as
	// \u00xx escapes. Recover those existing references without weakening the
	// checksum validation.
	legacyRaw := restoreDatabaseJSONHTMLEscapes(raw)
	if requestBodyReferenceMatches(reference, legacyRaw) {
		return nil
	}

	if len(raw) != reference.RawBytes {
		return fmt.Errorf("referenced request body length mismatch: got %d, want %d", len(raw), reference.RawBytes)
	}

	return errors.New("referenced request body checksum mismatch")
}

func requestBodyReferenceMatches(reference databaseRequestBodyReferenceEnvelope, raw []byte) bool {
	if len(raw) != reference.RawBytes {
		return false
	}

	_, digest := rawPayloadIdentity(raw)
	return digest == reference.SHA256
}

func restoreDatabaseJSONHTMLEscapes(raw []byte) []byte {
	restored := raw
	replacements := [...]struct {
		from []byte
		to   []byte
	}{
		{from: []byte(`\u003c`), to: []byte("<")},
		{from: []byte(`\u003e`), to: []byte(">")},
		{from: []byte(`\u0026`), to: []byte("&")},
		{from: []byte(`\u2028`), to: []byte("\u2028")},
		{from: []byte(`\u2029`), to: []byte("\u2029")},
	}
	for _, replacement := range replacements {
		restored = bytes.ReplaceAll(restored, replacement.from, replacement.to)
	}

	return restored
}

func isSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func isCompressedStoredPayload(raw []byte) bool {
	return storedPayloadHasMarker(raw, databasePayloadEnvelopePrefix, databasePayloadCodecMarker)
}

func storedPayloadHasMarker(raw, prefix []byte, marker string) bool {
	if bytes.HasPrefix(bytes.TrimSpace(raw), prefix) {
		return true
	}
	// PostgreSQL jsonb reorders object keys and adds spaces. Keep the cheap
	// serializer-prefix path, then identify normalized envelopes by their field.
	return bytes.Contains(raw, []byte(`"_axonhub_payload"`)) &&
		gjson.GetBytes(raw, "_axonhub_payload").String() == marker
}

func compressStoredPayloadForDatabase(ctx context.Context, raw objects.JSONRawMessage) objects.JSONRawMessage {
	compressed, err := CompressStoredPayload(raw)
	if err != nil {
		log.Warn(ctx, "Failed to compress database payload; storing original data", log.Cause(err))
		return raw
	}

	return compressed
}
