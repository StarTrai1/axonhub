package objects

import (
	"fmt"
	"io"
)

type ImageGenerationPolicy string

const (
	ImageGenerationNative        ImageGenerationPolicy = "native"
	ImageGenerationOtherChannels ImageGenerationPolicy = "other_channels"
)

func (p ChannelPolicies) EffectiveImageGenerationPolicy() ImageGenerationPolicy {
	if p.ImageGeneration == ImageGenerationOtherChannels {
		return ImageGenerationOtherChannels
	}
	return ImageGenerationNative
}

func (p ImageGenerationPolicy) MarshalGQL(w io.Writer) {
	_, _ = fmt.Fprintf(w, "%q", (ChannelPolicies{ImageGeneration: p}).EffectiveImageGenerationPolicy())
}

func (p *ImageGenerationPolicy) UnmarshalGQL(value any) error {
	s, ok := value.(string)
	if !ok || (s != "native" && s != "other_channels") {
		return fmt.Errorf("image generation policy must be native or other_channels")
	}
	*p = ImageGenerationPolicy(s)
	return nil
}

type CodexSpeedPolicy string

const (
	CodexSpeedAuto      CodexSpeedPolicy = "auto"
	CodexSpeedStandard  CodexSpeedPolicy = "standard"
	CodexSpeedFast      CodexSpeedPolicy = "fast"
	CodexSpeedUltrafast CodexSpeedPolicy = "ultrafast"
)

func (p ChannelPolicies) EffectiveCodexSpeedPolicy() CodexSpeedPolicy {
	switch p.CodexSpeed {
	case CodexSpeedStandard, CodexSpeedFast, CodexSpeedUltrafast:
		return p.CodexSpeed
	default:
		return CodexSpeedAuto
	}
}

func (p ChannelPolicies) CodexDefaultServiceTier() string {
	switch p.EffectiveCodexSpeedPolicy() {
	case CodexSpeedStandard:
		return "default"
	case CodexSpeedFast:
		return "priority"
	case CodexSpeedUltrafast:
		return "ultrafast"
	default:
		return ""
	}
}

func (p CodexSpeedPolicy) MarshalGQL(w io.Writer) {
	_, _ = fmt.Fprintf(w, "%q", (ChannelPolicies{CodexSpeed: p}).EffectiveCodexSpeedPolicy())
}

func (p *CodexSpeedPolicy) UnmarshalGQL(value any) error {
	s, ok := value.(string)
	if !ok || (s != "auto" && s != "standard" && s != "fast" && s != "ultrafast") {
		return fmt.Errorf("invalid Codex speed policy")
	}
	*p = CodexSpeedPolicy(s)
	return nil
}
