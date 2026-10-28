package httpclient

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func TestReadHTTPRequestRejectsOversizedZstdWindow(t *testing.T) {
	// A valid raw-block frame with an explicit 128 MiB window (descriptor
	// 0x88). The encoder shrinks small inputs, so use the wire fixture itself.
	compressed := []byte{0x28, 0xb5, 0x2f, 0xfd, 0x00, 0x88, 0x11, 0x00, 0x00, '{', '}'}
	var header zstd.Header
	require.NoError(t, header.Decode(compressed))
	require.EqualValues(t, 2*maxRequestZstdWindowSize, header.WindowSize)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(compressed))
	request.Header.Set("Content-Encoding", "zstd")
	result, err := ReadHTTPRequest(request)
	require.ErrorIs(t, err, zstd.ErrWindowSizeExceeded)
	require.Nil(t, result)
}

func TestReadHTTPRequestRejectsZstdExpansionPastBodyLimit(t *testing.T) {
	previous := maxRequestBodySize
	maxRequestBodySize = 1024
	t.Cleanup(func() { maxRequestBodySize = previous })
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	require.NoError(t, err)
	t.Cleanup(func() { encoder.Close() })
	body := encoder.EncodeAll(bytes.Repeat([]byte("a"), maxRequestBodySize+1), nil)
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
	request.Header.Set("Content-Encoding", "zstd")
	result, err := ReadHTTPRequest(request)
	require.ErrorIs(t, err, ErrRequestBodyTooLarge)
	require.Nil(t, result)
}
