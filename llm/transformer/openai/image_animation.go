package openai

import (
	"fmt"
	"strings"

	"github.com/looplj/axonhub/llm/transformer"
)

func validateImageAnimation(format string, frames, duration, compression, partials *int64) error {
	if !strings.EqualFold(strings.TrimSpace(format), "gif") {
		if frames != nil || duration != nil {
			return fmt.Errorf("%w: frame_count and frame_duration_ms require output_format=gif", transformer.ErrInvalidRequest)
		}
		return nil
	}
	if frames != nil && *frames < 2 {
		return fmt.Errorf("%w: frame_count must be at least 2", transformer.ErrInvalidRequest)
	}
	if duration != nil && *duration < 20 {
		return fmt.Errorf("%w: frame_duration_ms must be at least 20", transformer.ErrInvalidRequest)
	}
	if compression != nil || partials != nil {
		return fmt.Errorf("%w: GIF output does not support output_compression or partial_images", transformer.ErrInvalidRequest)
	}
	// The guide and API reference currently disagree on animation upper limits.
	// Preserve the requested values and let the selected upstream enforce them.
	return nil
}
