package orchestrator

import (
 "strings"
 "testing"

 "github.com/stretchr/testify/require"

 "github.com/looplj/axonhub/internal/ent"
 "github.com/looplj/axonhub/internal/server/biz"
 "github.com/looplj/axonhub/llm/httpclient"
)

func TestMissingGeminiCacheDoesNotOpenModelCircuit(t *testing.T) {
 cb:=biz.NewModelCircuitBreaker()
 out:=&PersistentOutboundTransformer{state:&PersistenceState{OriginalModel:"gemini-3-pro",RoutingPolicy:EffectiveRoutingPolicy{LoadBalancerStrategy:biz.LoadBalancerStrategyCircuitBreaker},CurrentCandidate:&ChannelModelsCandidate{Channel:&biz.Channel{Channel:&ent.Channel{ID:42}}}}}
 tracker:=withModelCircuitBreaker(out,cb)
 for i:=0;i<20;i++ { tracker.OnOutboundRawError(t.Context(),&httpclient.Error{StatusCode:403,Body:[]byte(`{"error":{"message":"CachedContent not found (or permission denied)"}}`)}) }
 stats:=cb.GetModelCircuitBreakerStats(t.Context(),42,"gemini-3-pro")
 if stats!=nil { require.Zero(t,stats.ConsecutiveFailures);require.NotEqual(t,biz.StateOpen,stats.State) }
}

func TestErrorLogsRedactGoogleKeyFormats(t *testing.T) {
 for _,secret:=range []string{"AIza"+strings.Repeat("x",35),"AQ.Ab"+strings.Repeat("x",48)} {
  result:=string(sanitizeResponseBody([]byte("upstream rejected "+secret),1024))
  require.NotContains(t,result,secret)
  require.Contains(t,result,"[REDACTED]")
 }
}
