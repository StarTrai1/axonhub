package orchestrator

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestUnifiedHistoryEmptyInstructionsTransport(t *testing.T) {
	source := []byte(`{"model":"gpt-6-astra","input":[{"role":"user","content":"exact original"},{"type":"compaction_trigger"}]}`)
	ref := &remoteCompactionReference{ID: "cmp_transport", EncryptedContent: "synthetic-cipher", Index: 0}
	for _, instructions := range []string{`""`, `null`, `"changed instruction"`, `" "`} {
		t.Run(instructions, func(t *testing.T) {
			body := []byte(`{"model":"gpt-6-astra","instructions":` + instructions + `,"input":[{"type":"compaction","id":"cmp_transport","encrypted_content":"synthetic-cipher"},{"role":"user","content":"continue"}]}`)
			for _, unified := range []bool{false, true} {
				var got []byte
				var restored bool
				var err error
				if unified {
					got, restored, err = spliceUnifiedCompactionSource(body, source, ref)
				} else {
					got, restored, err = spliceNativeCompactionSource(body, source, ref, nativeCompactionReplayMaxBytes)
				}
				require.NoError(t, err)
				require.Equal(t, instructions == `""` || instructions == `null`, restored)
				if restored {
					require.Equal(t, "exact original", gjson.GetBytes(got, "input.0.content").String())
					require.Equal(t, "continue", gjson.GetBytes(got, "input.1.content").String())
					require.Equal(t, instructions, gjson.GetBytes(got, "instructions").Raw)
				} else {
					require.Equal(t, body, got, "a nonempty or whitespace prompt change must remain explicit")
				}
			}
		})
	}
}
