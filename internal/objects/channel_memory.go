package objects

import (
	"fmt"
	"io"
)

type CodexMemoryPolicy string

const (
	CodexMemoryNormal CodexMemoryPolicy = "normal"
	CodexMemoryExclude CodexMemoryPolicy = "exclude"
	CodexMemoryOnly CodexMemoryPolicy = "only"
)

func (p ChannelPolicies) EffectiveCodexMemoryPolicy() CodexMemoryPolicy {
	switch p.CodexMemory {
	case CodexMemoryExclude, CodexMemoryOnly:
		return p.CodexMemory
	default:
		return CodexMemoryNormal
	}
}

func (p ChannelPolicies) AllowsCodexMemoryTask(memory bool) bool {
	switch p.EffectiveCodexMemoryPolicy() {
	case CodexMemoryExclude:
		return !memory
	case CodexMemoryOnly:
		return memory
	default:
		return true
	}
}

func (p CodexMemoryPolicy) MarshalGQL(w io.Writer) {
	_, _ = fmt.Fprintf(w, "%q", (ChannelPolicies{CodexMemory: p}).EffectiveCodexMemoryPolicy())
}

func (p *CodexMemoryPolicy) UnmarshalGQL(value any) error {
	s, ok := value.(string)
	if !ok || (s != "normal" && s != "exclude" && s != "only") {
		return fmt.Errorf("invalid Codex memory policy")
	}
	*p = CodexMemoryPolicy(s)
	return nil
}
