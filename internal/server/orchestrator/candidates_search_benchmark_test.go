package orchestrator

import (
	"testing"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
)

var searchCandidateBenchmarkSink []*ChannelModelsCandidate

func BenchmarkSearchOnlyGenerationFilter(b *testing.B) {
	candidates := make([]*ChannelModelsCandidate, 32)
	for i := range candidates {
		candidates[i] = &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{Type: channel.TypeCodex}}}
	}
	req := &llm.Request{RequestType: llm.RequestTypeChat, APIFormat: llm.APIFormatOpenAIResponse}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		searchCandidateBenchmarkSink = filterSearchOnlyCandidates(candidates, req)
	}
}
