package biz

import (
	"fmt"
	"strings"

	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/httpclient"
)

// NormalizeHTTPTransportSettings validates and canonicalizes per-channel
// upstream transport settings before persistence.
func NormalizeHTTPTransportSettings(settings *objects.ChannelSettings) error {
	if settings == nil {
		return nil
	}

	protocol := strings.ToLower(strings.TrimSpace(settings.HTTPProtocol))
	switch protocol {
	case "", string(httpclient.HTTPProtocolAuto):
		settings.HTTPProtocol = ""
	case string(httpclient.HTTPProtocolHTTP1):
		settings.HTTPProtocol = protocol
	default:
		return fmt.Errorf("invalid HTTP protocol %q: must be auto or http1", settings.HTTPProtocol)
	}

	shards := settings.HTTP2ConnectionShards
	if shards < 0 || shards > httpclient.MaxHTTP2ConnectionShards {
		return fmt.Errorf(
			"invalid HTTP/2 connection shards %d: must be 0 or between 1 and %d",
			shards,
			httpclient.MaxHTTP2ConnectionShards,
		)
	}
	if settings.HTTPProtocol == string(httpclient.HTTPProtocolHTTP1) && shards > 1 {
		return fmt.Errorf("HTTP/2 connection shards must be 1 when HTTP/1.1 is forced")
	}

	// Zero is the compact persisted representation of the default single shard.
	if shards == 1 {
		settings.HTTP2ConnectionShards = 0
	}

	return nil
}
