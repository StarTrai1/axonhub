package gemini

import (
	"slices"
	"strconv"
	"strings"

	"github.com/looplj/axonhub/llm"
)

func geminiToolMediaPart(part llm.MessageContentPart) *Part {
	switch part.Type {
	case "image_url":
		return convertImageURLToGeminiPart(part.ImageURL)
	case "document":
		return convertDocumentURLToGeminiPart(part.Document)
	case "input_audio":
		return convertAudioToGeminiPart(part.InputAudio)
	case "video_url":
		return convertVideoURLToGeminiPart(part.VideoURL)
	default:
		return nil
	}
}

func placeGeminiToolMedia(content *Content, model string) {
	model = strings.TrimPrefix(strings.ToLower(model), "models/")
	version := strings.TrimPrefix(model, "gemini-")
	segments := strings.FieldsFunc(version, func(r rune) bool { return r == '.' || r == '-' })
	if len(segments) == 0 {
		return
	}
	major, err := strconv.Atoi(segments[0])
	if !strings.HasPrefix(model, "gemini-") || err != nil || major < 3 {
		return
	}
	response := content.Parts[0].FunctionResponse
	parts := content.Parts[:1]
	for _, part := range content.Parts[1:] {
		// FunctionResponsePart has no fileData, audio or video variant. Keep
		// those as normal parts after the grouped function responses, without
		// fetching arbitrary URLs or serializing media as text.
		if part.InlineData != nil && (strings.HasPrefix(part.InlineData.MIMEType, "image/") || part.InlineData.MIMEType == "application/pdf") {
			response.Parts = append(response.Parts, &FunctionResponsePart{InlineData: part.InlineData})
		} else {
			parts = append(parts, part)
		}
	}
	content.Parts = parts
}

func appendGeminiToolResponse(previous, next *Content) {
	// Keep parallel responses adjacent before any legacy/out-of-band media.
	index := 0
	for index < len(previous.Parts) && previous.Parts[index].FunctionResponse != nil {
		index++
	}
	previous.Parts = slices.Insert(previous.Parts, index, next.Parts[0])
	previous.Parts = append(previous.Parts, next.Parts[1:]...)
}
