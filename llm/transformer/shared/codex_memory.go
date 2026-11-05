package shared

import (
	"context"
	"net/http"
	"strings"

	"github.com/tidwall/gjson"
)

type CodexMemoryTask string

const (
	CodexMemoryNone CodexMemoryTask = ""
	CodexMemoryExtract CodexMemoryTask = "extract"
	CodexMemoryConsolidate CodexMemoryTask = "consolidate"
)

type codexMemoryTaskKey struct{}

func WithCodexMemoryTask(ctx context.Context, task CodexMemoryTask) context.Context {
	return context.WithValue(ctx, codexMemoryTaskKey{}, task)
}

func GetCodexMemoryTask(ctx context.Context) CodexMemoryTask {
	task, _ := ctx.Value(codexMemoryTaskKey{}).(CodexMemoryTask)
	return task
}

// ReadCodexMemoryTask reads purpose, never model, effort or prompt text. A valid
// current-frame purpose supersedes the WebSocket handshake in its entirety.
// known also includes explicit foreground purpose, which must not inherit an
// older background task when following a previous response.
func ReadCodexMemoryTask(headers http.Header, body []byte) (task CodexMemoryTask, known bool) {
	client := gjson.GetBytes(body, "client_metadata")
	turn := gjson.Parse(headers.Get("X-Codex-Turn-Metadata"))
	subagent := strings.TrimSpace(headers.Get("X-Openai-Subagent"))
	if embedded := client.Get("x-codex-turn-metadata"); embedded.Type == gjson.String && gjson.Valid(embedded.String()) {
		if current := gjson.Parse(embedded.String()); current.IsObject() {
			turn = current
			// Do not carry a stale handshake subagent into a foreground frame.
			subagent = ""
		}
	}
	if value := client.Get("x-openai-subagent"); value.Type == gjson.String {
		subagent = strings.TrimSpace(value.String())
	}
	kind, source := turn.Get("request_kind").String(), turn.Get("thread_source").String()
	if kind == "memory" {
		return CodexMemoryExtract, true
	}
	if source == "memory_consolidation" || subagent == "memory_consolidation" {
		return CodexMemoryConsolidate, true
	}
	return CodexMemoryNone, kind != "" || source != "" || subagent != ""
}
