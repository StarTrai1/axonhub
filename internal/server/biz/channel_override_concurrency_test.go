package biz

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
)

func TestChannelOverrideConcurrentInitialization(t *testing.T) {
	ch := &Channel{Channel: &ent.Channel{Settings: &objects.ChannelSettings{
		OverrideParameters: `{"temperature":0.7}`,
		HeaderOverrideOperations: []objects.OverrideOperation{{Op: objects.OverrideOpSet, Path: "X-Synthetic", Value: "fixture"}},
	}}}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			for range 50 {
				ch.GetBodyOverrideOperations()
				ch.GetHeaderOverrideOperations()
			}
		})
	}
	wg.Wait()
	require.Len(t, ch.GetBodyOverrideOperations(), 1)
	require.Len(t, ch.GetHeaderOverrideOperations(), 1)
	require.Equal(t, "temperature", ch.GetBodyOverrideOperations()[0].Path)
	require.Equal(t, "fixture", ch.GetHeaderOverrideOperations()[0].Value)
}
