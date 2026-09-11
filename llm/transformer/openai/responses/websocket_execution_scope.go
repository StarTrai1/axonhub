package responses

import (
	"encoding/json"
	"strings"

	"github.com/looplj/axonhub/llm/transformer/shared"
)

// A root session can have a foreground response, child-agent responses, and
// background memory work in flight together. Only sequential work in the same
// thread may wait on the same pooled connection.
func codexExecutionPoolIdentity(metadata shared.CodexRequestMetadata) string {
	kind := strings.ToLower(metadata.RequestKind)
	lane := ""
	switch kind {
	case "", "turn", "prewarm", "compaction":
		// These requests belong to the same sequential conversation.
	default:
		lane = "kind:" + kind
	}
	if kind == "" && metadata.Subagent != "" {
		lane = "subagent:" + metadata.Subagent
	}
	if metadata.ThreadID == "" && lane == "" {
		return ""
	}
	// Hash a tuple so separators inside client identities cannot collide, and
	// pool keys do not retain arbitrarily large metadata strings.
	identity, _ := json.Marshal([2]string{metadata.ThreadID, lane})
	return hashSecret(string(identity))
}
