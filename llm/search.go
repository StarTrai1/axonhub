package llm

// SearchRequest preserves the standalone OpenAI search request body. The
// endpoint is still evolving, so transformers only inspect the model field and
// pass all other JSON fields through unchanged.
type SearchRequest struct {
	Raw []byte `json:"-"`
}

// SearchResponse preserves the standalone OpenAI search response body so
// clients receive new result fields without waiting for AxonHub schema updates.
type SearchResponse struct {
	Raw []byte `json:"-"`
}
