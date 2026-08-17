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
)

type databasePayloadEnvelope struct {
	Marker     string `json:"_axonhub_payload"`
	Codec      string `json:"codec"`
	Version    int    `json:"version"`
	RawBytes   int    `json:"raw_bytes"`
	SHA256     string `json:"sha256"`
	Compressed string `json:"data"`
}

var (
	databasePayloadEnvelopePrefix = []byte(`{"_axonhub_payload":"axonhub.payload"`)
	databasePayloadEncoderPool    sync.Pool
	databasePayloadDecoderPool    sync.Pool
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
	zstdEncoder := encoder.(*zstd.Encoder)
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
	zstdDecoder := decoder.(*zstd.Decoder)
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
