package orchestrator

import (
	"crypto/sha256"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"github.com/tiktoken-go/tokenizer"

	"github.com/looplj/axonhub/internal/server/biz"

	lru "github.com/hashicorp/golang-lru/v2"
)

// Archive limits and immutable segment boundaries use bytes. Model admission
// uses tokens. JSON is an intentionally overcomplete rendering of the prompt;
// reserve another 25% for server formatting and tokenizer differences. This is
// an estimate, not an assertion about a provider's private prompt serialization.
// Unknown model families retain the conservative byte bound.
type unifiedHistoryMeasure func([]byte) int

func unifiedModelMeasure(model string) unifiedHistoryMeasure {
	model = strings.ToLower(strings.TrimSpace(model))
	model = strings.TrimSuffix(model, "-fast")
	if biz.BundledModelContextLimit(model) > 0 && (strings.HasPrefix(model, "gpt-5") || strings.HasPrefix(model, "gpt-6") ||
		strings.HasPrefix(model, "gpt-4o") || strings.HasPrefix(model, "gpt-4.1") ||
		model == "o3" || strings.HasPrefix(model, "o3-") || model == "o4-mini") {
		return unifiedTextTokens
	}
	return func(raw []byte) int { return len(raw) }
}

var unifiedTokenizer = sync.OnceValue(func() tokenizer.Codec {
	codec, _ := tokenizer.Get(tokenizer.O200kBase)
	return codec
})

var unifiedTokenCounts = sync.OnceValue(func() *lru.Cache[[sha256.Size]byte, int] {
	cache, _ := lru.New[[sha256.Size]byte, int](8192)
	return cache
})

func unifiedTextTokens(raw []byte) int {
	count := 0
	// Bounded pieces prevent pathological long strings from causing quadratic
	// BPE work. The safety allowance also covers piece-boundary differences.
	for len(raw) > 0 {
		n := min(len(raw), 1024)
		for n < len(raw) && n > 0 && !utf8.RuneStart(raw[n]) {
			n--
		}
		if n == 0 {
			return count + len(raw)
		}
		piece := raw[:n]
		key := sha256.Sum256(piece)
		tokens, found := unifiedTokenCounts().Get(key)
		if !found {
			var err error
			tokens, err = unifiedTokenizer().Count(string(piece))
			if err != nil {
				tokens = len(piece)
			}
			unifiedTokenCounts().Add(key, tokens)
		}
		count += tokens
		raw = raw[n:]
	}
	return count + (count+3)/4
}

func (measure unifiedHistoryMeasure) item(item json.RawMessage) int {
	if measure == nil {
		return unifiedItemSize(item)
	}
	visual := 0
	for _, field := range []string{"content", "output"} {
		for i, part := range gjson.GetBytes(item, field).Array() {
			if part.Get("type").String() == "input_image" && part.Get("image_url").Type == gjson.String {
				visual += unifiedImageAllowance
				// Measure the image's protocol wrapper, never its base64 bytes as text.
				path := field + "." + strconv.Itoa(i) + ".image_url"
				item, _ = sjson.SetBytes(item, path, "")
			}
		}
	}
	return measure(item) + visual + 16
}

func (measure unifiedHistoryMeasure) items(items []json.RawMessage) int {
	size := 2
	for _, item := range items {
		size += measure.item(item)
	}
	return size
}
