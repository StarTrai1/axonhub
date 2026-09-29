package httpclient

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeForwardResponseHeaders(t *testing.T) {
	tests := []struct {
		name string
		src  http.Header
		want string
	}{
		{name: "true", src: http.Header{"x-reasoning-included": []string{" TRUE "}}, want: "true"},
		{name: "false", src: http.Header{ReasoningIncludedHeader: []string{"false"}}},
		{name: "missing", src: http.Header{"Set-Cookie": []string{"secret=1"}}},
		{name: "conflicting duplicates", src: http.Header{
			ReasoningIncludedHeader: []string{"true"},
			"x-reasoning-included":  []string{"false"},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := http.Header{ReasoningIncludedHeader: []string{"stale"}}
			got := MergeForwardResponseHeaders(dst, tt.src)

			require.Equal(t, tt.want, got.Get(ReasoningIncludedHeader))
			require.Empty(t, got.Get("Set-Cookie"))
		})
	}
}

func TestMergeForwardResponseHeadersRemovesCaseVariants(t *testing.T) {
	dst := http.Header{
		"x-reasoning-included": []string{"stale"},
		"X-REASONING-INCLUDED": []string{"also-stale"},
	}

	got := MergeForwardResponseHeaders(dst, http.Header{ReasoningIncludedHeader: []string{"true"}})

	require.Equal(t, []string{"true"}, got[ReasoningIncludedHeader])
	require.NotContains(t, got, "x-reasoning-included")
	require.NotContains(t, got, "X-REASONING-INCLUDED")
}

func TestResponseHeaderCaptureCopiesHeaders(t *testing.T) {
	ctx, capture := WithResponseHeaderCapture(context.Background())
	headers := http.Header{ReasoningIncludedHeader: []string{"true"}}
	RecordResponseHeaders(ctx, headers)
	headers.Set(ReasoningIncludedHeader, "false")

	got := capture.Headers()
	got.Set(ReasoningIncludedHeader, "false")
	require.Equal(t, "true", capture.Headers().Get(ReasoningIncludedHeader))
	capture.Reset()
	require.Empty(t, capture.Headers())
}
