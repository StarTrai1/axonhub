package biz

import (
 "testing"
 "github.com/stretchr/testify/require"
)

func TestMissingGeminiCacheDoesNotEvaluateDisableRules(t *testing.T) {
 svc:=&ChannelService{}
 matched,acted:=svc.evaluateAutoDisableForFailure(t.Context(),&PerformanceRecord{ChannelID:1,ResponseStatusCode:403,ErrorMessage:"CachedContent not found (or permission denied)"})
 require.False(t,matched)
 require.False(t,acted)
}
