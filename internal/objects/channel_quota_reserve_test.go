package objects

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelQuotaReservePersistenceAndValidation(t *testing.T) {
	for _, percent := range []int{-1, 0, 1, 90, 100, 101} {
		settings := &ChannelSettings{QuotaReserve: &ChannelQuotaReserve{FiveHourPercent: &percent, AllowMemory: true}}
		err := settings.QuotaReserve.Validate()
		if percent < 1 || percent > 100 {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		raw, err := json.Marshal(settings)
		require.NoError(t, err)
		var restored ChannelSettings
		require.NoError(t, json.Unmarshal(raw, &restored))
		require.Equal(t, settings.QuotaReserve, restored.QuotaReserve)
		require.NoError(t, json.Unmarshal([]byte(`{"quotaReserve":null}`), &restored))
		require.Nil(t, restored.QuotaReserve)
	}
	var old ChannelSettings
	require.NoError(t, json.Unmarshal([]byte(`{}`), &old))
	require.NoError(t, old.QuotaReserve.Validate())
}
