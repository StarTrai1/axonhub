package responses

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai"
)

func TestNamedToolChoiceAcrossChatAndResponses(t *testing.T) {
	const docs = `{"type":"namespace","name":"docs","tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{}}}]}`
	const crm = `{"type":"namespace","name":"crm","tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{}}}]}`
	const plain = `{"type":"function","name":"lookup","parameters":{"type":"object","properties":{}}}`
	for _, scenario := range []struct {
		name   string
		tools  string
		choice string
		want   string
	}{
		{"plain", plain, `{"type":"function","name":"lookup"}`, "lookup"},
		{"unique namespace", docs, `{"type":"function","name":"lookup"}`, "docs__lookup"},
		{"explicit namespace", docs + "," + crm, `{"type":"function","name":"lookup","namespace":"crm"}`, "crm__lookup"},
		{"explicit namespace before plain name", plain + "," + docs, `{"type":"function","name":"lookup","namespace":"docs"}`, "docs__lookup"},
		{"exact plain name", docs + "," + plain, `{"type":"function","name":"lookup"}`, "lookup"},
		{"already flat", docs, `{"type":"function","name":"docs__lookup"}`, "docs__lookup"},
		{"ambiguous local name", docs + "," + crm, `{"type":"function","name":"lookup"}`, "lookup"},
		{
			"local name contains namespace prefix",
			`{"type":"namespace","name":"docs","tools":[{"type":"function","name":"docs__lookup","parameters":{"type":"object","properties":{}}}]}`,
			`{"type":"function","name":"docs__lookup","namespace":"docs"}`,
			"docs__docs__lookup",
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for _, persisted := range []bool{false, true} {
				raw := []byte(`{"model":"test","input":"hi","tools":[` + scenario.tools + `],"tool_choice":` + scenario.choice + `}`)
				req, err := NewInboundTransformer().TransformRequest(t.Context(), &httpclient.Request{Body: raw})
				require.NoError(t, err)
				if persisted {
					encoded, err := json.Marshal(req)
					require.NoError(t, err)
					req = &llm.Request{}
					require.NoError(t, json.Unmarshal(encoded, req))
					req.RawRequest = nil
				}
				before, err := json.Marshal(req)
				require.NoError(t, err)
				chat := openai.RequestFromLLM(t.Context(), req, openai.ReasoningFieldNone)
				require.Equal(t, "function", chat.ToolChoice.NamedToolChoice.Type)
				require.Equal(t, scenario.want, chat.ToolChoice.NamedToolChoice.Function.Name)
				if scenario.name != "ambiguous local name" {
					var names []string
					for _, tool := range chat.Tools {
						names = append(names, tool.Function.Name)
					}
					require.Contains(t, names, scenario.want)
				}
				after, err := json.Marshal(req)
				require.NoError(t, err)
				require.JSONEq(t, string(before), string(after), "Chat conversion must not change a subsequent Responses attempt")

				native, err := NewOutboundTransformer("https://example.com/v1", "test-key")
				require.NoError(t, err)
				wire, err := native.TransformRequest(t.Context(), req)
				require.NoError(t, err)
				var body map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(wire.Body, &body))
				require.JSONEq(t, scenario.choice, string(body["tool_choice"]))
			}
		})
	}
}

func TestToolChoiceStringDecodeClearsPreviousNamedSelection(t *testing.T) {
	var choice ToolChoice
	require.NoError(t, json.Unmarshal([]byte(`{"type":"function","name":"lookup","namespace":"docs"}`), &choice))
	require.NoError(t, json.Unmarshal([]byte(`"auto"`), &choice))
	require.Nil(t, choice.Type)
	require.Nil(t, choice.Name)
	require.Nil(t, choice.Namespace)
	encoded, err := json.Marshal(choice)
	require.NoError(t, err)
	require.JSONEq(t, `"auto"`, string(encoded))
}
