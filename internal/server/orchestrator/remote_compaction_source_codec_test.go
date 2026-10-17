package orchestrator

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeCompactionBinaryEncodingCompatibility(t *testing.T) {
	aead, err := newLocalCompactionCipher("synthetic-installation-key")
	require.NoError(t, err)
	aad := []byte("synthetic-owner-checkpoint-and-ciphertext")
	compressed := bytes.Repeat([]byte{0, 255, 128, 1}, 256*1024)
	legacy, err := sealCompactionText(aead, aad, base64.RawStdEncoding.EncodeToString(compressed), nativeCompactionMaxSealedTextBytes())
	require.NoError(t, err)
	packed, err := sealNativeCompactionSource(aead, aad, compressed)
	require.NoError(t, err)
	require.Less(t, len(packed), len(legacy)*76/100)
	t.Logf("same source bytes=%d legacy stored=%d binary stored=%d", len(compressed), len(legacy), len(packed))
	for _, sealed := range []string{legacy, packed} {
		got, err := openNativeCompactionSource(aead, aad, sealed)
		require.NoError(t, err)
		require.Equal(t, compressed, got)
		_, err = openNativeCompactionSource(aead, []byte("different-owner-or-checkpoint"), sealed)
		require.Error(t, err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(packed, nativeCompactionBinaryPrefix))
	require.NoError(t, err)
	decoded[len(decoded)-1] ^= 1
	_, err = openNativeCompactionSource(aead, aad, nativeCompactionBinaryPrefix+base64.RawURLEncoding.EncodeToString(decoded))
	require.Error(t, err)
	for _, malformed := range []string{nativeCompactionBinaryPrefix, nativeCompactionBinaryPrefix + "!", nativeCompactionBinaryPrefix + "AA", strings.Replace(packed, nativeCompactionBinaryPrefix, localCompactionSealedReferencePrefix, 1)} {
		_, err = openNativeCompactionSource(aead, aad, malformed)
		require.Error(t, err)
	}
	_, err = sealNativeCompactionSource(aead, aad, make([]byte, nativeCompactionMaxCheckpointBytes+1))
	require.Error(t, err)
}

func BenchmarkNativeCompactionSourceEncoding(b *testing.B) {
	aead, err := newLocalCompactionCipher("synthetic-installation-key")
	if err != nil {
		b.Fatal(err)
	}
	aad := []byte("synthetic-owner-checkpoint-and-ciphertext")
	compressed := bytes.Repeat([]byte{0, 255, 128, 1}, 256*1024)
	for _, variant := range []string{"legacy", "binary"} {
		b.Run(variant, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				var err error
				if variant == "legacy" {
					_, err = sealCompactionText(aead, aad, base64.RawStdEncoding.EncodeToString(compressed), nativeCompactionMaxSealedTextBytes())
				} else {
					_, err = sealNativeCompactionSource(aead, aad, compressed)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
