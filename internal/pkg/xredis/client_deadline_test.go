package xredis

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

func TestNewClientHonorsCommandDeadline(t *testing.T) {
	for _, mode := range []string{"address", "URL"} {
		t.Run(mode, func(t *testing.T) {
			server := miniredis.RunT(t)
			config := Config{Addr: server.Addr()}
			if mode == "URL" {
				config = Config{URL: "redis://" + server.Addr() + "?read_timeout=5s&max_retries=-1"}
			}
			client, err := NewClient(config)
			require.NoError(t, err)
			t.Cleanup(func() { _ = client.Close() })
			commands := server.CommandCount()
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			started := time.Now()

			// The server accepts this command but cannot reply until its own
			// three-second timeout. The caller's deadline must end socket I/O.
			err = client.BLPop(ctx, 3*time.Second, "empty-list").Err()
			require.Error(t, err)
			require.Greater(t, server.CommandCount(), commands)
			require.Less(t, time.Since(started), 2*time.Second)
			if !errors.Is(err, context.DeadlineExceeded) {
				var timeout net.Error
				require.ErrorAs(t, err, &timeout)
				require.True(t, timeout.Timeout())
			}
		})
	}
}
