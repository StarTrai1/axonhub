package transformer

import (
	"fmt"

	"github.com/looplj/axonhub/llm"
)

func ValidateImageBytes(img *llm.ImageRequest) error {
	if len(img.Markup) > 0 || img.MarkupReference != nil {
		return fmt.Errorf("%w: this image provider does not support markup overlays", ErrUnsupportedConversion)
	}
	if len(img.ImageReferences) > len(img.Images) || (img.MaskReference != nil && len(img.Mask) == 0) {
		return fmt.Errorf("%w: this image provider requires image bytes; HTTP URLs and file IDs are unsupported", ErrInvalidRequest)
	}
	return nil
}
