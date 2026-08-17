package biz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/objects"
)

func TestNormalizeHTTPTransportSettings(t *testing.T) {
	tests := []struct {
		name         string
		settings     *objects.ChannelSettings
		wantProtocol string
		wantShards   int
		wantErr      string
	}{
		{name: "nil settings"},
		{name: "defaults", settings: &objects.ChannelSettings{}},
		{
			name:         "canonicalize auto single shard",
			settings:     &objects.ChannelSettings{HTTPProtocol: " AUTO ", HTTP2ConnectionShards: 1},
			wantProtocol: "",
			wantShards:   0,
		},
		{
			name:         "force http1",
			settings:     &objects.ChannelSettings{HTTPProtocol: "HTTP1", HTTP2ConnectionShards: 1},
			wantProtocol: "http1",
			wantShards:   0,
		},
		{
			name:         "sharded http2",
			settings:     &objects.ChannelSettings{HTTP2ConnectionShards: 4},
			wantProtocol: "",
			wantShards:   4,
		},
		{
			name:     "invalid protocol",
			settings: &objects.ChannelSettings{HTTPProtocol: "http3"},
			wantErr:  "must be auto or http1",
		},
		{
			name:     "too many shards",
			settings: &objects.ChannelSettings{HTTP2ConnectionShards: 9},
			wantErr:  "must be 0 or between 1 and 8",
		},
		{
			name:     "http1 rejects sharding",
			settings: &objects.ChannelSettings{HTTPProtocol: "http1", HTTP2ConnectionShards: 2},
			wantErr:  "must be 1 when HTTP/1.1 is forced",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NormalizeHTTPTransportSettings(tt.settings)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			if tt.settings != nil {
				require.Equal(t, tt.wantProtocol, tt.settings.HTTPProtocol)
				require.Equal(t, tt.wantShards, tt.settings.HTTP2ConnectionShards)
			}
		})
	}
}
