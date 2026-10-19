package orchestrator

import (
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func BenchmarkUnifiedHistoryCompression(b *testing.B) {
	body := []byte(strings.Repeat(`{"role":"assistant","content":"original evidence and exact tool arguments"}`, 900))[:historyBlockBytes]
	for _, pooled := range []bool{false, true} {
		name := "fresh"
		if pooled {
			name = "pooled"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(body)))
			for b.Loop() {
				if pooled {
					if _, err := compressHistoryRecord(body); err != nil {
						b.Fatal(err)
					}
				} else {
					encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithEncoderLevel(zstd.SpeedFastest), zstd.WithWindowSize(historyBlockBytes))
					if err != nil {
						b.Fatal(err)
					}
					_ = encoder.EncodeAll(body, nil)
					if err := encoder.Close(); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
