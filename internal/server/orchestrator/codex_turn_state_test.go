package orchestrator

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm/httpclient"
)

func codexTurnStateTestOutbound(channelID int, sessionID string) *PersistentOutboundTransformer {
	return &PersistentOutboundTransformer{state: &PersistenceState{
		APIKey:     &ent.APIKey{ID: 7},
		RawRequest: &httpclient.Request{Headers: http.Header{"Session-Id": []string{sessionID}}},
		CurrentCandidate: &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{
			ID:          channelID,
			Type:        channel.TypeCodex,
			Credentials: objects.ChannelCredentials{OAuth: &objects.OAuthCredentials{AccessToken: "test-token"}},
		}}},
	}}
}

func codexTurnStateAPIKeyTestOutbound(channelID int, sessionID string) *PersistentOutboundTransformer {
	outbound := codexTurnStateTestOutbound(channelID, sessionID)
	outbound.state.CurrentCandidate.Channel.Credentials = objects.ChannelCredentials{APIKey: "test-key"}

	return outbound
}

func TestCodexTurnStateIsolation_PreservesSameChannelAndStripsCrossChannel(t *testing.T) {
	tracker := newCodexTurnStateTracker()
	origin := codexTurnStateTestOutbound(11, "session-1")
	tracker.noteSuccessfulResponse(origin.state, http.Header{codexTurnStateHeader: []string{"state-a"}})

	tests := []struct {
		name      string
		channelID int
		want      string
	}{
		{name: "same channel", channelID: 11, want: "state-a"},
		{name: "different channel", channelID: 12, want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			outbound := codexTurnStateTestOutbound(test.channelID, "session-1")
			request := &httpclient.Request{Headers: http.Header{codexTurnStateHeader: []string{"state-a"}}}

			processed, err := applyCodexTurnStateIsolation(outbound, tracker).
				OnOutboundRawRequest(context.Background(), request)

			require.NoError(t, err)
			require.Equal(t, test.want, processed.Headers.Get(codexTurnStateHeader))
		})
	}
}

func TestCodexTurnStateIsolation_UnknownOrExpiredOriginPassesThrough(t *testing.T) {
	tracker := newCodexTurnStateTracker()
	outbound := codexTurnStateTestOutbound(12, "session-unknown")
	request := &httpclient.Request{Headers: http.Header{codexTurnStateHeader: []string{"external-state"}}}

	processed, err := applyCodexTurnStateIsolation(outbound, tracker).
		OnOutboundRawRequest(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "external-state", processed.Headers.Get(codexTurnStateHeader))

	key := codexTurnStateSessionKey(outbound.state, outbound.state.RawRequest.Headers)
	tracker.origins.Store(key, codexTurnStateOrigin{channelID: 11, expiresAt: time.Now().Add(-time.Second)})
	processed, err = applyCodexTurnStateIsolation(outbound, tracker).
		OnOutboundRawRequest(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, "external-state", processed.Headers.Get(codexTurnStateHeader))
}

func TestCodexTurnStateIsolation_ThirdPartyAPIKeyChannelPassesThrough(t *testing.T) {
	tracker := newCodexTurnStateTracker()
	origin := codexTurnStateTestOutbound(11, "session-1")
	tracker.noteSuccessfulResponse(origin.state, http.Header{codexTurnStateHeader: []string{"state-a"}})
	outbound := codexTurnStateAPIKeyTestOutbound(12, "session-1")
	request := &httpclient.Request{Headers: http.Header{codexTurnStateHeader: []string{"state-a"}}}

	processed, err := applyCodexTurnStateIsolation(outbound, tracker).
		OnOutboundRawRequest(context.Background(), request)

	require.NoError(t, err)
	require.Equal(t, "state-a", processed.Headers.Get(codexTurnStateHeader))
}

func TestCodexTurnStateTracker_DoesNotStoreHeaderValue(t *testing.T) {
	tracker := newCodexTurnStateTracker()
	outbound := codexTurnStateTestOutbound(11, "session-1")
	tracker.noteSuccessfulResponse(outbound.state, http.Header{codexTurnStateHeader: []string{"sensitive-state"}})

	key := codexTurnStateSessionKey(outbound.state, outbound.state.RawRequest.Headers)
	raw, ok := tracker.origins.Load(key)
	require.True(t, ok)
	require.Equal(t, 11, raw.(codexTurnStateOrigin).channelID)
	require.NotContains(t, key, "sensitive-state")
}
