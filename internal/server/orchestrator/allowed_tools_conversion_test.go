package orchestrator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
	"github.com/looplj/axonhub/llm/transformer/deepseek"
	"github.com/looplj/axonhub/llm/transformer/gemini"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/looplj/axonhub/llm/transformer/openai/responses"
)

func TestResponsesAllowedToolsAcrossProtocols(t *testing.T) {
	for _, target := range []struct {
		name        string
		constructor func(string, string) (transformer.Outbound, error)
		namesPath   string
		modePath    string
		autoMode    string
		required    string
	}{
		{"Chat", openai.NewOutboundTransformer, "tools.#.function.name", "tool_choice", "auto", "required"},
		{"DeepSeek", deepseek.NewOutboundTransformer, "tools.#.function.name", "tool_choice", "auto", "required"},
		{"Anthropic", anthropic.NewOutboundTransformer, "tools.#.name", "tool_choice.type", "auto", "any"},
		{"Gemini", gemini.NewOutboundTransformer, "tools.0.functionDeclarations.#.name", "toolConfig.functionCallingConfig.mode", "AUTO", "ANY"},
	} {
		for _, mode := range []string{"auto", "required"} {
			for _, persisted := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/persisted=%v", target.name, mode, persisted), func(t *testing.T) {
					// Historical calls remain valid even when they are no longer callable.
					// The allowlist order differs from the original declaration order.
					raw := []byte(fmt.Sprintf(`{
						"model":"test",
						"input":[
							{"role":"user","content":"earlier turn"},
							{"type":"function_call","name":"b","call_id":"call_previous","arguments":"{}"},
							{"type":"function_call_output","name":"b","call_id":"call_previous","output":"history-result"},
							{"role":"user","content":"use the allowed tools"}
						],
						"tools":[
							{"type":"function","name":"b","parameters":{"type":"object","properties":{}}},
							{"type":"namespace","name":"docs","tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{}}}]},
							{"type":"function","name":"a","parameters":{"type":"object","properties":{}}},
							{"type":"custom","name":"shell","format":{"type":"text"}}
						],
						"tool_choice":{"type":"allowed_tools","mode":%q,"tools":[{"type":"function","name":"a"},{"type":"function","name":"lookup"}]}
					}`, mode))
					req, err := responses.NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: raw})
					require.NoError(t, err)
					if persisted {
						encoded, err := json.Marshal(req)
						require.NoError(t, err)
						req = &llm.Request{}
						require.NoError(t, json.Unmarshal(encoded, req))
					}
					before, err := json.Marshal(req)
					require.NoError(t, err)
					outbound, err := target.constructor("https://example.com/v1", "test")
					require.NoError(t, err)
					processor := newAllowedToolsTestProcessor(t, outbound)

					// Snapshot native serialization before trying a different protocol.
					processor.state.CurrentCandidateIndex = 1
					nativeBefore, err := processor.TransformRequest(t.Context(), req)
					require.NoError(t, err)
					processor.state.CurrentCandidateIndex = 0
					wire, err := processor.TransformRequest(t.Context(), req)
					require.NoError(t, err)
					var names []string
					for _, name := range gjson.GetBytes(wire.Body, target.namesPath).Array() {
						names = append(names, name.String())
					}
					require.Equal(t, []string{"docs__lookup", "a"}, names)
					wantMode := target.autoMode
					if mode == "required" {
						wantMode = target.required
					}
					require.Equal(t, wantMode, gjson.GetBytes(wire.Body, target.modePath).String())
					require.Contains(t, string(wire.Body), `"name":"b"`, "keep historical function calls")
					require.Contains(t, string(wire.Body), "history-result", "keep historical tool outputs")

					after, err := json.Marshal(req)
					require.NoError(t, err)
					require.JSONEq(t, string(before), string(after), "conversion must not mutate the shared request")
					processor.state.CurrentCandidateIndex = 1
					nativeAfter, err := processor.TransformRequest(t.Context(), req)
					require.NoError(t, err)
					require.JSONEq(t, string(nativeBefore.Body), string(nativeAfter.Body), "native retry retains the full catalog and structured restriction")
				})
			}
		}
	}
}

func TestResponsesAllowedToolsRejectSemanticLoss(t *testing.T) {
	const tools = `[{"type":"function","name":"a"},{"type":"function","name":"b"}]`
	const choice = `{"type":"allowed_tools","mode":"auto","tools":[{"type":"function","name":"a"}]}`
	for _, scenario := range []struct {
		name   string
		tools  string
		choice string
		input  string
	}{
		{"missing mode", tools, `{"type":"allowed_tools","tools":[{"type":"function","name":"a"}]}`, `"hi"`},
		{"invalid mode", tools, `{"type":"allowed_tools","mode":"none","tools":[{"type":"function","name":"a"}]}`, `"hi"`},
		{"empty allowlist", tools, `{"type":"allowed_tools","mode":"auto","tools":[]}`, `"hi"`},
		{"missing declaration", tools, `{"type":"allowed_tools","mode":"auto","tools":[{"type":"function","name":"missing"}]}`, `"hi"`},
		{"duplicate selection", tools, `{"type":"allowed_tools","mode":"auto","tools":[{"type":"function","name":"a"},{"type":"function","name":"a"}]}`, `"hi"`},
		{"ambiguous namespace", `[{"type":"namespace","name":"docs","tools":[{"type":"function","name":"a"}]},{"type":"namespace","name":"crm","tools":[{"type":"function","name":"a"}]}]`, choice, `"hi"`},
		{"non-function selection", tools, `{"type":"allowed_tools","mode":"auto","tools":[{"type":"web_search"}]}`, `"hi"`},
		{"custom history", tools, choice, `[{"type":"custom_tool_call","name":"shell","call_id":"call_custom","input":"pwd"},{"type":"custom_tool_call_output","call_id":"call_custom","output":"/workspace"},{"role":"user","content":"hi"}]`},
		{"native history", tools, choice, `[{"type":"mcp_list_tools","id":"mcp_1","server_label":"demo","tools":[]},{"role":"user","content":"hi"}]`},
		{"opaque compaction", tools, choice, `[{"type":"compaction","id":"cmp_1","encrypted_content":"opaque"},{"role":"user","content":"hi"}]`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			raw := []byte(fmt.Sprintf(`{"model":"test","input":%s,"tools":%s,"tool_choice":%s}`, scenario.input, scenario.tools, scenario.choice))
			req, err := responses.NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: raw})
			require.NoError(t, err)
			before, err := json.Marshal(req)
			require.NoError(t, err)
			outbound, err := openai.NewOutboundTransformer("https://example.com/v1", "test")
			require.NoError(t, err)
			processor := newAllowedToolsTestProcessor(t, outbound)
			processor.state.CurrentCandidateIndex = 1
			nativeBefore, err := processor.TransformRequest(t.Context(), req)
			require.NoError(t, err)
			processor.state.CurrentCandidateIndex = 0
			wire, err := processor.TransformRequest(t.Context(), req)
			require.ErrorIs(t, err, transformer.ErrInvalidRequest)
			require.Contains(t, err.Error(), "allowed_tools")
			require.Nil(t, wire, "do not send a request with relaxed tool constraints")
			after, err := json.Marshal(req)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after))

			// A native attempt still receives the original choice and history. Its
			// own endpoint remains responsible for validating native restrictions.
			processor.state.CurrentCandidateIndex = 1
			native, err := processor.TransformRequest(t.Context(), req)
			require.NoError(t, err)
			require.JSONEq(t, string(nativeBefore.Body), string(native.Body))
			if scenario.name == "custom history" {
				require.Contains(t, string(native.Body), `"custom_tool_call"`)
				require.Contains(t, string(native.Body), `"custom_tool_call_output"`)
			}
			if scenario.name == "native history" {
				require.Contains(t, string(native.Body), `"mcp_list_tools"`)
			}
		})
	}
}

func TestResponsesAllowedToolsExactNamePrecedesNamespace(t *testing.T) {
	raw := []byte(`{"model":"test","input":"hi","tools":[{"type":"namespace","name":"docs","tools":[{"type":"function","name":"lookup"}]},{"type":"function","name":"lookup"}],"tool_choice":{"type":"allowed_tools","mode":"required","tools":[{"type":"function","name":"lookup"}]}}`)
	req, err := responses.NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: raw})
	require.NoError(t, err)
	outbound, err := openai.NewOutboundTransformer("https://example.com/v1", "test")
	require.NoError(t, err)
	processor := newAllowedToolsTestProcessor(t, outbound)
	wire, err := processor.TransformRequest(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1), gjson.GetBytes(wire.Body, "tools.#").Int())
	require.Equal(t, "lookup", gjson.GetBytes(wire.Body, "tools.0.function.name").String())
	require.Equal(t, "required", gjson.GetBytes(wire.Body, "tool_choice").String())
}

func TestChatAllowedToolsPreservesNativeChoiceAcrossFallbacks(t *testing.T) {
	for _, mode := range []string{"auto", "required"} {
		t.Run(mode, func(t *testing.T) {
			raw := []byte(fmt.Sprintf(`{"model":"test","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"b"}},{"type":"function","function":{"name":"a"}}],"tool_choice":{"type":"allowed_tools","allowed_tools":{"mode":%q,"tools":[{"type":"function","function":{"name":"a"}}]}}}`, mode))
			req, err := openai.NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: raw, Headers: http.Header{"Content-Type": {"application/json"}}})
			require.NoError(t, err)
			before, err := json.Marshal(req)
			require.NoError(t, err)
			chat, err := openai.NewOutboundTransformer("https://example.com/v1", "test")
			require.NoError(t, err)
			processor := newAllowedToolsTestProcessor(t, chat)
			wire, err := processor.TransformRequest(t.Context(), req)
			require.NoError(t, err)
			require.Equal(t, int64(2), gjson.GetBytes(wire.Body, "tools.#").Int())
			require.JSONEq(t, gjson.GetBytes(raw, "tool_choice").Raw, gjson.GetBytes(wire.Body, "tool_choice").Raw)

			anthropicOutbound, err := anthropic.NewOutboundTransformer("https://example.com/v1", "test")
			require.NoError(t, err)
			fallback := newAllowedToolsTestProcessor(t, anthropicOutbound)
			converted, err := fallback.TransformRequest(t.Context(), req)
			require.NoError(t, err)
			require.Equal(t, int64(1), gjson.GetBytes(converted.Body, "tools.#").Int())
			require.Equal(t, "a", gjson.GetBytes(converted.Body, "tools.0.name").String())
			wantMode := mode
			if mode == "required" {
				wantMode = "any"
			}
			require.Equal(t, wantMode, gjson.GetBytes(converted.Body, "tool_choice.type").String())

			after, err := json.Marshal(req)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after))
			retried, err := processor.TransformRequest(t.Context(), req)
			require.NoError(t, err)
			require.JSONEq(t, string(wire.Body), string(retried.Body))
		})
	}
}

func newAllowedToolsTestProcessor(t *testing.T, target transformer.Outbound) *PersistentOutboundTransformer {
	t.Helper()
	native, err := responses.NewOutboundTransformer("https://example.com/v1", "test")
	require.NoError(t, err)
	candidates := make([]*ChannelModelsCandidate, 0, 2)
	for index, outbound := range []transformer.Outbound{target, native} {
		candidates = append(candidates, &ChannelModelsCandidate{
			Channel: &biz.Channel{
				Channel:  &ent.Channel{ID: index + 1, Name: outbound.APIFormat().String()},
				Outbound: outbound,
			},
			Models:    []biz.ChannelModelEntry{{RequestModel: "test", ActualModel: "test"}},
			APIFormat: outbound.APIFormat().String(),
		})
	}
	return &PersistentOutboundTransformer{state: &PersistenceState{ChannelModelsCandidates: candidates}}
}
