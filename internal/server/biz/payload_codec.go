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

// CompressStoredPayload losslessly compresses large JSON payloads for database persistence.
func CompressStoredPayload(raw []byte) (objects.JSONRawMessage, error) {
	if len(raw) < databasePayloadCompressionThreshold || isCompressedStoredPayload(raw) {
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

	if base64.RawStdEncoding.EncodedLen(len(compressed)) >= len(raw) {
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
	if len(envelope) >= len(raw) {
		return objects.JSONRawMessage(raw), nil
	}

	return objects.JSONRawMessage(envelope), nil
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
	if parentBytes != len(candidateRaw) {
		return objects.JSONRawMessage(candidateRaw), false, nil
	}

	digest := sha256.Sum256(candidateRaw)
	candidateSHA256 := hex.EncodeToString(digest[:])
	if parentSHA256 != candidateSHA256 {
		return objects.JSONRawMessage(candidateRaw), false, nil
	}

	reference, err := json.Marshal(databaseRequestBodyReferenceEnvelope{
		Marker:    databaseRequestBodyReferenceMarker,
		Version:   databaseRequestBodyReferenceVersion,
		RequestID: parentRequestID,
		RawBytes:  len(candidateRaw),
		SHA256:    candidateSHA256,
	})
	if err != nil {
		return nil, false, fmt.Errorf("marshal request body reference: %w", err)
	}

	return objects.JSONRawMessage(reference), true, nil
}

func storedPayloadIdentity(raw []byte) (int, string, error) {
	if !isCompressedStoredPayload(raw) {
		digest := sha256.Sum256(raw)
		return len(raw), hex.EncodeToString(digest[:]), nil
	}

	var envelope databasePayloadEnvelope
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

func decodeStoredRequestBodyReference(raw []byte) (databaseRequestBodyReferenceEnvelope, bool, error) {
	if !bytes.HasPrefix(bytes.TrimSpace(raw), databaseRequestBodyReferencePrefix) {
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
	if len(raw) != reference.RawBytes {
		return fmt.Errorf("referenced request body length mismatch: got %d, want %d", len(raw), reference.RawBytes)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != reference.SHA256 {
		return errors.New("referenced request body checksum mismatch")
	}

	return nil
}

func isSHA256Hex(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func isCompressedStoredPayload(raw []byte) bool {
	return bytes.HasPrefix(bytes.TrimSpace(raw), databasePayloadEnvelopePrefix)
}

func compressStoredPayloadForDatabase(ctx context.Context, raw objects.JSONRawMessage) objects.JSONRawMessage {
	compressed, err := CompressStoredPayload(raw)
	if err != nil {
		log.Warn(ctx, "Failed to compress database payload; storing original data", log.Cause(err))
		return raw
	}

	return compressed
}
