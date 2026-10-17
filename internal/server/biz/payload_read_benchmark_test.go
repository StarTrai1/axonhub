package biz

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/looplj/axonhub/internal/objects"
)

func BenchmarkStoredPayloadIdentity(b *testing.B) {
	encoded, err := json.Marshal(databasePayloadEnvelope{
		Marker: databasePayloadCodecMarker, Codec: "zstd", Version: databasePayloadCodecVersion,
		RawBytes: 8 << 20, SHA256: strings.Repeat("a", 64), Compressed: strings.Repeat("YWFh", 256*1024),
	})
	if err != nil { b.Fatal(err) }
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := storedPayloadIdentity(encoded); err != nil { b.Fatal(err) }
	}
}

func BenchmarkExternalResponseMarkerCheck(b *testing.B) {
	body := objects.JSONRawMessage(`{"id":"response-fixture","output":"` + strings.Repeat("x", 1024*1024) + `"}`)
	b.ReportAllocs()
	for b.Loop() {
		if isExternalResponseBodyMarker(body) { b.Fatal("response mistaken for marker") }
	}
}
