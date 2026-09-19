package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestCodexPromptCacheBreakpointsCoverInputAndToolOutput(t *testing.T) {
	const body = `{"precision":9007199254740993,"input":[{"type":"message","role":"user","prompt_cache_breakpoint":{"mode":"explicit"},"content":[{"type":"input_text","text":"keep prompt_cache_breakpoint in text","prompt_cache_breakpoint":null}]},{"type":"function_call_output","call_id":"call_a","prompt_cache_breakpoint":true,"output":[{"type":"input_text","text":"tool result","prompt_cache_breakpoint":{"mode":"explicit"}},{"type":"input_image","image_url":"data:image/png;base64,AA==","prompt_cache_breakpoint":false},{"type":"input_file","file_id":"file_a","metadata":{"prompt_cache_breakpoint":"business data"}}]},{"type":"function_call_output","call_id":"call_b","output":"{\"prompt_cache_breakpoint\":true}"}]} `
	for _, kind := range []channel.Type{channel.TypeCodex, channel.TypeOpenai} {
		t.Run(string(kind), func(t *testing.T) {
			outbound := &PersistentOutboundTransformer{state: &PersistenceState{
				CurrentCandidate: &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{Type: kind}}},
			}}
			original := []byte(body)
			request := &httpclient.Request{Body: original}
			got, err := stripUnsupportedCodexPromptCacheOptions(outbound).OnOutboundRawRequest(t.Context(), request)
			require.NoError(t, err)
			require.Equal(t, body, string(original))
			if kind != channel.TypeCodex {
				require.Equal(t, body, string(got.Body))
				return
			}
			for _, path := range []string{"input.0.prompt_cache_breakpoint", "input.0.content.0.prompt_cache_breakpoint", "input.1.prompt_cache_breakpoint", "input.1.output.0.prompt_cache_breakpoint", "input.1.output.1.prompt_cache_breakpoint"} {
				require.False(t, gjson.GetBytes(got.Body, path).Exists(), path)
			}
			for _, path := range []string{"precision", "input.0.content.0.text", "input.1.call_id", "input.1.output.0.text", "input.1.output.1.image_url", "input.1.output.2", "input.2"} {
				require.Equal(t, gjson.Get(body, path).Raw, gjson.GetBytes(got.Body, path).Raw, path)
			}
			once := string(got.Body)
			got, err = stripUnsupportedCodexPromptCacheOptions(outbound).OnOutboundRawRequest(t.Context(), got)
			require.NoError(t, err)
			require.Equal(t, once, string(got.Body))
		})
	}
}
