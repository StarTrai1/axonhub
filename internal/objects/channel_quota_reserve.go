package objects

import "fmt"

// ChannelQuotaReserve reserves provider quota for selected background workloads.
// Missing windows disable that limit; this is independent of hard exhaustion.
type ChannelQuotaReserve struct {
	FiveHourPercent *int `json:"fiveHourPercent,omitempty"`
	SevenDayPercent *int `json:"sevenDayPercent,omitempty"`
	AllowSearch bool `json:"allowSearch,omitempty"`
	AllowImage bool `json:"allowImage,omitempty"`
	AllowMemory bool `json:"allowMemory,omitempty"`
}

func (r *ChannelQuotaReserve) Validate() error {
	if r == nil { return nil }
	for _, percent := range []*int{r.FiveHourPercent, r.SevenDayPercent} {
		if percent != nil && (*percent < 1 || *percent > 100) {
			return fmt.Errorf("quota reserve thresholds must be between 1 and 100 percent")
		}
	}
	return nil
}
