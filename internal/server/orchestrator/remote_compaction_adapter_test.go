package orchestrator

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildLocalCompactionRequest(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"before"}]},
			{"type":"compaction_trigger"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"must be removed"}]}
		],
		"stream":false,
		"client_metadata":{
			"thread_id":"thread-1",
			"x-codex-turn-metadata":"{\"compaction\":{\"implementation\":\"responses_compaction_v2\"}}"
		}
	}`)

	got, err := buildLocalCompactionRequest(body)
	require.NoError(t, err)

	envelope, input, err := decodeResponsesInput(got)
	require.NoError(t, err)
	require.Len(t, input, 2)
	require.Equal(t, "message", rawInputItemType(input[1]))
	require.NotContains(t, string(got), remoteCompactionTriggerType)
	require.Contains(t, string(input[1]), "CONTEXT CHECKPOINT COMPACTION")
	require.JSONEq(t, "true", string(envelope["stream"]))

	var metadata map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(envelope["client_metadata"], &metadata))
	var turnMetadata string
	require.NoError(t, json.Unmarshal(metadata["x-codex-turn-metadata"], &turnMetadata))
	require.JSONEq(t, `{"compaction":{"implementation":"responses"}}`, turnMetadata)
}

func TestReplaceRemoteCompactionWithLocalSummary(t *testing.T) {
	body := []byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"retained"}]},
			{"id":"cmp_old","type":"compaction","encrypted_content":"old"},
			{"type":"message","role":"developer","content":[{"type":"input_text","text":"context"}]},
			{"id":"cmp_new","type":"compaction","encrypted_content":"new"},
			{"type":"message","role":"user","content":[{"type":"input_text","text":"continue"}]}
		],
		"stream":true,
		"client_metadata":{"thread_id":"thread-1"}
	}`)

	got, err := replaceRemoteCompactionWithLocalSummary(body, "handoff summary")
	require.NoError(t, err)

	_, input, err := decodeResponsesInput(got)
	require.NoError(t, err)
	require.Len(t, input, 4)
	require.Equal(t, []string{"message", "message", "message", "message"}, []string{
		rawInputItemType(input[0]),
		rawInputItemType(input[1]),
		rawInputItemType(input[2]),
		rawInputItemType(input[3]),
	})
	require.Contains(t, string(input[2]), localCompactionSummaryPrefix)
	require.Contains(t, string(input[2]), "handoff summary")
	require.NotContains(t, string(got), `"type":"compaction"`)
	require.Contains(t, string(input[3]), "continue")
}

func TestParseRemoteCompactionRequestUsesLatestItem(t *testing.T) {
	ref, threadID, model, err := parseRemoteCompactionRequest([]byte(`{
		"model":"gpt-5.6-sol",
		"input":[
			{"id":"cmp_old","type":"compaction","encrypted_content":"old"},
			{"id":"cmp_new","type":"compaction_summary","encrypted_content":"new"}
		],
		"client_metadata":{"thread_id":"thread-1"}
	}`))

	require.NoError(t, err)
	require.Equal(t, "thread-1", threadID)
	require.Equal(t, "gpt-5.6-sol", model)
	require.Equal(t, &remoteCompactionReference{
		ID:               "cmp_new",
		EncryptedContent: "new",
		Index:            1,
	}, ref)
	require.NotEmpty(t, remoteCompactionCacheKey(ref))
}

func TestCompactionResponseAndSummaryExtraction(t *testing.T) {
	response := []byte(`{
		"output":[
			{"id":"cmp_123","type":"compaction","encrypted_content":"opaque"},
			{"type":"message","role":"assistant","content":[
				{"type":"output_text","text":"part one"},
				{"type":"output_text","text":"part two"}
			]}
		]
	}`)

	require.True(t, responseContainsCompactionID(response, "cmp_123"))
	require.False(t, responseContainsCompactionID(response, "cmp_other"))
	require.Equal(t, "part one\npart two", extractAssistantOutputText(response))
}
