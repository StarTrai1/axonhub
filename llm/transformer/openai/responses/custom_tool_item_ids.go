package responses

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/looplj/axonhub/llm"
)

const customToolItemIDMetadataKey = "responses_custom_tool_item_id"

func customToolItemMetadata(id string) map[string]any {
	if id == "" {
		return nil
	}
	return map[string]any{customToolItemIDMetadataKey: id}
}

// Item identity and tool-result correlation are separate Responses fields.
// Preserve native identity through conversion; synthesize a stable typed ID
// only when the source has no valid custom-tool item identity.
func customToolItemID(call llm.ToolCall) string {
	if id, ok := call.TransformerMetadata[customToolItemIDMetadataKey].(string); ok && strings.HasPrefix(id, "ctc_") {
		return id
	}
	if strings.HasPrefix(call.ID, "ctc_") {
		return call.ID
	}
	seed := call.ID
	if call.ResponseCustomToolCall != nil && call.ResponseCustomToolCall.CallID != "" {
		seed = call.ResponseCustomToolCall.CallID
	}
	digest := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("ctc_%x", digest[:24])
}
