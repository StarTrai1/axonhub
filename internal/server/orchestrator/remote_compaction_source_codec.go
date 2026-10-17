package orchestrator

import (
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
)

// Stored native sources contain compressed binary data. A separate versioned
// envelope avoids base64-encoding the plaintext before encrypting it. Client
// compaction references and their existing text envelope are unchanged.
const nativeCompactionBinaryPrefix = "axonhub-native-source-v1."

func nativeCompactionBinaryAAD(aad []byte) []byte {
	bound := make([]byte, 0, len(aad)+len(nativeCompactionBinaryPrefix))
	bound = append(bound, aad...)
	return append(bound, nativeCompactionBinaryPrefix...)
}

func sealNativeCompactionSource(aead cipher.AEAD, aad, compressed []byte) (string, error) {
	if aead == nil || len(aad) == 0 || len(compressed) == 0 || len(compressed) > nativeCompactionMaxCheckpointBytes {
		return "", errors.New("invalid native compaction source encryption input")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, compressed, nativeCompactionBinaryAAD(aad))
	return nativeCompactionBinaryPrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func openNativeCompactionSource(aead cipher.AEAD, aad []byte, sealed string) ([]byte, error) {
	if !strings.HasPrefix(sealed, nativeCompactionBinaryPrefix) {
		// Existing databases retain the original text envelope and inner
		// base64. Read it without rewriting or dropping retained history.
		text, err := openCompactionText(aead, aad, sealed, nativeCompactionMaxSealedTextBytes())
		if err != nil {
			return nil, err
		}
		return base64.RawStdEncoding.DecodeString(text)
	}
	if aead == nil || len(aad) == 0 {
		return nil, invalidLocalCompactionReference()
	}
	encoded := strings.TrimPrefix(sealed, nativeCompactionBinaryPrefix)
	limit := nativeCompactionMaxCheckpointBytes + aead.NonceSize() + aead.Overhead()
	if len(encoded) > base64.RawURLEncoding.EncodedLen(limit) {
		return nil, invalidLocalCompactionReference()
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(payload) <= aead.NonceSize()+aead.Overhead() {
		return nil, invalidLocalCompactionReference()
	}
	nonce := payload[:aead.NonceSize()]
	compressed, err := aead.Open(nil, nonce, payload[aead.NonceSize():], nativeCompactionBinaryAAD(aad))
	if err != nil || len(compressed) == 0 || len(compressed) > nativeCompactionMaxCheckpointBytes {
		return nil, invalidLocalCompactionReference()
	}
	return compressed, nil
}
