package objects

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexMemoryPolicyCompatibility(t *testing.T) {
	for _, raw := range []string{`{}`, `{"codexMemory":null}`, `{"codexMemory":"future"}`} {
		var p ChannelPolicies
		require.NoError(t, json.Unmarshal([]byte(raw), &p))
		require.Equal(t, CodexMemoryNormal, p.EffectiveCodexMemoryPolicy())
		require.True(t, p.AllowsCodexMemoryTask(false))
		require.True(t, p.AllowsCodexMemoryTask(true))
	}
	for _, policy := range []CodexMemoryPolicy{CodexMemoryNormal, CodexMemoryExclude, CodexMemoryOnly} {
		p := ChannelPolicies{CodexMemory: policy}
		require.Equal(t, policy != CodexMemoryOnly, p.AllowsCodexMemoryTask(false))
		require.Equal(t, policy != CodexMemoryExclude, p.AllowsCodexMemoryTask(true))
		var decoded CodexMemoryPolicy
		require.NoError(t, decoded.UnmarshalGQL(string(policy)))
		var buf bytes.Buffer
		decoded.MarshalGQL(&buf)
		require.Equal(t, `"`+string(policy)+`"`, buf.String())
	}
	var p CodexMemoryPolicy
	require.Error(t, p.UnmarshalGQL("future"))
}
