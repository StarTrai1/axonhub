package biz

import "github.com/looplj/axonhub/internal/ent/channel"

// IsCodexSearchOnly is an opt-in routing restriction, independent of the
// channel's advertised generation models or its Responses transport.
func (ch *Channel) IsCodexSearchOnly() bool {
	return ch != nil && ch.Channel != nil && ch.Type == channel.TypeCodex &&
		ch.Settings != nil && ch.Settings.CodexSearchOnly
}
