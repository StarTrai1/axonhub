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
	// A streaming encoder omits the single-segment shortcut, so the advertised
	// window is independent of this tiny, otherwise valid JSON request.
	var compressed bytes.Buffer
	encoder, err := zstd.NewWriter(&compressed, zstd.WithWindowSize(2*maxRequestZstdWindowSize), zstd.WithEncoderConcurrency(1))
	require.NoError(t, err)
	_, err = encoder.Write([]byte(`{"model":"test","input":"hello"}`))
	require.NoError(t, err)
	require.NoError(t, encoder.Close())
	request := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(compressed.Bytes()))
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
