package anthropic

import (
	"encoding/json"
	"fmt"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/internal/pkg/xurl"
	"github.com/looplj/axonhub/llm/transformer"
)

const anthropicDocumentMetadataKey = "anthropic_document"

func documentBlockToLLMPart(block MessageContentBlock) (llm.MessageContentPart, error) {
	if block.Source == nil {
		return llm.MessageContentPart{}, fmt.Errorf("%w: document source is required", transformer.ErrInvalidRequest)
	}
	source := block.Source
	document := &llm.DocumentURL{MIMEType: source.MediaType}
	switch source.Type {
	case "base64":
		if source.Data == "" || source.MediaType == "" {
			return llm.MessageContentPart{}, fmt.Errorf("%w: document data and media_type are required", transformer.ErrInvalidRequest)
		}
		document.URL = xurl.BuildDataURL(source.MediaType, source.Data, true)
	case "url":
		if source.URL == "" {
			return llm.MessageContentPart{}, fmt.Errorf("%w: document URL is required", transformer.ErrInvalidRequest)
		}
		document.URL = source.URL
	default:
		return llm.MessageContentPart{}, fmt.Errorf("%w: document source must be base64 or url for protocol conversion", transformer.ErrInvalidRequest)
	}
	raw, err := json.Marshal(block)
	if err != nil {
		return llm.MessageContentPart{}, fmt.Errorf("encode document metadata: %w", err)
	}
	return llm.MessageContentPart{
		Type:                "document",
		Document:            document,
		CacheControl:        convertToLLMCacheControl(block.CacheControl),
		TransformerMetadata: map[string]any{anthropicDocumentMetadataKey: json.RawMessage(raw)},
	}, nil
}

func documentPartToAnthropicBlock(part llm.MessageContentPart) (MessageContentBlock, bool) {
	if part.Document == nil || part.Document.URL == "" {
		return MessageContentBlock{}, false
	}
	block := MessageContentBlock{Type: "document"}
	if raw, ok := part.TransformerMetadata[anthropicDocumentMetadataKey]; ok {
		if data, err := json.Marshal(raw); err == nil {
			var original MessageContentBlock
			if json.Unmarshal(data, &original) == nil && original.Type == "document" {
				if restored, err := documentBlockToLLMPart(original); err == nil && *restored.Document == *part.Document {
					block = original
				}
			}
		}
	}
	block.CacheControl = convertToAnthropicCacheControl(part.CacheControl)
	if len(block.RawDocument) == 0 {
		if parsed := xurl.ParseDataURL(part.Document.URL); parsed != nil {
			block.Source = &ImageSource{Type: "base64", MediaType: parsed.MediaType, Data: parsed.Data}
		} else {
			block.Source = &ImageSource{Type: "url", URL: part.Document.URL}
		}
	}
	return block, true
}
