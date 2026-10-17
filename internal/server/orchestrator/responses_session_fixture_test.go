package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

// Exercise the same session identity, owner selection and attempt preparation
// used by Process. The retired store.prepare algorithm must not shadow runtime
// behavior in these replay regressions.
func prepareResponsesSessionForTest(t *testing.T, store *responsesSessionStore, ctx context.Context, body []byte) ([]byte, string) {
	t.Helper()
	c := newResponsesContinuation(store, body)
	ctx = store.withContinuationSession(ctx, c.previous)
	p := &PersistentOutboundTransformer{state: &PersistenceState{ResponsesContinuation: c}}
	require.NoError(t, selectResponsesContinuationCandidate(ctx, p, &llm.Request{}))
	target := responsesAttemptTarget{origin: shared.ResponsesOrigin{Format: string(llm.APIFormatOpenAIResponse)}, forwardsID: true}
	if c.owner != nil {
		// A metadata-only fixture may still continue on its known native origin.
		target.origin = c.owner.Origin
	}
	prepared, err := prepareResponsesAttemptBody(ctx, p, target)
	require.NoError(t, err)
	sessionID, _ := shared.GetSessionID(ctx)
	return prepared, sessionID
}
