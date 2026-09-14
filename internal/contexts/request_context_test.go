package contexts

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
)

func TestCloneRequestContextSeparatesMutableValues(t *testing.T) {
	connection := WithAPIKey(t.Context(), &ent.APIKey{ID: 1})
	connection = WithProjectID(connection, 2)
	connection = WithRequestID(connection, "handshake")
	first := CloneRequestContext(connection)
	second := CloneRequestContext(connection)
	first = WithAPIKey(first, &ent.APIKey{ID: 3})
	first = WithProjectID(first, 4)
	first = WithRequestID(first, "first")
	first = WithChannelAPIKey(first, "first-channel-key")
	AddError(first, errors.New("first request error"))
	second = WithChannelAPIKey(second, "second-channel-key")

	for _, ctx := range []context.Context{connection, second} {
		key, ok := GetAPIKey(ctx)
		require.True(t, ok)
		require.Equal(t, 1, key.ID)
		projectID, ok := GetProjectID(ctx)
		require.True(t, ok)
		require.Equal(t, 2, projectID)
		requestID, ok := GetRequestID(ctx)
		require.True(t, ok)
		require.Equal(t, "handshake", requestID)
		require.Empty(t, GetErrors(ctx))
	}
	_, present := GetChannelAPIKey(connection)
	require.False(t, present)
	firstKey, _ := GetChannelAPIKey(first)
	secondKey, _ := GetChannelAPIKey(second)
	require.Equal(t, "first-channel-key", firstKey)
	require.Equal(t, "second-channel-key", secondKey)
}
