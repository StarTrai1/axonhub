package codex

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestCodexWebSearchHistoryDeclaration(t *testing.T) {
	for _, transport := range []string{responses.TransportHTTP, responses.TransportWebSocket} {
		for _, lite := range []bool{false, true} {
			t.Run(transport+"/lite="+strconv.FormatBool(lite), func(t *testing.T) {
				out := newTestCodexOutbound(t)
				out.transport = transport
				body := []byte(`{"model":"gpt-6.1-sol","tools":[],"input":[{"type":"web_search_call","id":"ws_history","status":"completed","action":{"type":"search","query":"prior query"}},{"type":"message","role":"user","content":"Summarize this history."}]}`)
				req := &httpclient.Request{Body: body, Headers: make(http.Header)}
				if lite {
					req.Headers.Set(responses.ResponsesLiteHeader, "true")
				}
				wire := out.FinalizeTransportRequest(req)
				toolPath := "tools.0"
				if lite {
					toolPath = "input.2.tools.0"
					require.Empty(t, gjson.GetBytes(wire.Body, "tools").Array())
					require.Equal(t, "additional_tools", gjson.GetBytes(wire.Body, "input.2.type").String())
				}
				require.Equal(t, "web_search", gjson.GetBytes(wire.Body, toolPath+".type").String())
				require.Equal(t, "false", gjson.GetBytes(wire.Body, toolPath+".external_web_access").Raw)
				require.Equal(t, "none", gjson.GetBytes(wire.Body, "tool_choice").String())
				require.Equal(t, gjson.GetBytes(body, "input.0").Raw, gjson.GetBytes(wire.Body, "input.0").Raw)
				require.Equal(t, body, req.Body)
				require.Equal(t, wire.Body, out.FinalizeTransportRequest(wire).Body)
			})
		}
	}
}

func TestCodexWebSearchHistoryKeepsCatalogIdentityAndCompactionTrigger(t *testing.T) {
	out := newTestCodexOutbound(t)
	body := []byte(`{"model":"gpt-6.1-sol","tool_choice":"required","input":[{"id":"at_existing","type":"additional_tools","role":"developer","tools":[{"type":"namespace","name":"functions","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]}]},{"type":"web_search_call","id":"ws_history"},{"type":"compaction_trigger"}]}`)
	req := &httpclient.Request{Body: body, Headers: make(http.Header)}
	req.Headers.Set(responses.ResponsesLiteHeader, "true")
	wire := out.normalizeWebSearchHistory(req)
	require.Equal(t, gjson.GetBytes(body, "input.0").Raw, gjson.GetBytes(wire.Body, "input.0").Raw)
	require.Equal(t, "additional_tools", gjson.GetBytes(wire.Body, "input.2.type").String())
	require.Equal(t, "compaction_trigger", gjson.GetBytes(wire.Body, "input.3.type").String())
	require.Equal(t, "required", gjson.GetBytes(wire.Body, "tool_choice").String())
	require.Equal(t, body, req.Body)
}

func TestCodexWebSearchHistoryScope(t *testing.T) {
	for _, tc := range []struct {
		name, body, requestType string
		relay                   bool
	}{
		{"no history", `{"input":[{"type":"message","role":"user","content":"web_search_call"}]}`, "", false},
		{"existing declaration", `{"tools":[{"type":"web_search","external_web_access":true}],"input":[{"type":"web_search_call"}]}`, "", false},
		{"existing catalog declaration", `{"input":[{"type":"additional_tools","tools":[{"type":"web_search_preview"}]},{"type":"web_search_call"}]}`, "", false},
		{"required without tools", `{"tool_choice":"required","input":[{"type":"web_search_call"}]}`, "", false},
		{"invalid tools", `{"tools":{},"input":[{"type":"web_search_call"}]}`, "", false},
		{"compact", `{"input":[{"type":"web_search_call"}]}`, llm.RequestTypeCompact.String(), false},
		{"relay", `{"input":[{"type":"web_search_call"}]}`, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := newTestCodexOutbound(t)
			if tc.relay {
				out.baseURL = "https://relay.example/v1"
				out.official = false
			}
			req := &httpclient.Request{Body: []byte(tc.body), RequestType: tc.requestType}
			require.Same(t, req, out.normalizeWebSearchHistory(req))
		})
	}
}
