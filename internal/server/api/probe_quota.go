package api

import (
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
	"go.uber.org/fx"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/providerquotastatus"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/biz/provider_quota"
)

type ProbeQuotaHandlersParams struct {
	fx.In

	Ent           *ent.Client
	SystemService *biz.SystemService
}

// ProbeQuotaHandlers exposes only reset timestamps for a key's explicitly
// pinned channel. It never queries upstream or returns account/credential data.
type ProbeQuotaHandlers struct {
	params ProbeQuotaHandlersParams
}

func NewProbeQuotaHandlers(params ProbeQuotaHandlersParams) *ProbeQuotaHandlers {
	return &ProbeQuotaHandlers{params: params}
}

type probeQuotaWindow struct {
	Window      string     `json:"window"`
	NextResetAt *time.Time `json:"next_reset_at"`
}

func pinnedProbeChannel(key *ent.APIKey) (int, bool) {
	if key == nil || key.Key == biz.NoAuthAPIKeyValue || key.Edges.Project == nil || key.ProjectID != key.Edges.Project.ID {
		return 0, false
	}
	profile := key.GetActiveProfile()
	if profile == nil || len(profile.ChannelIDs) != 1 || profile.ChannelIDs[0] <= 0 {
		return 0, false
	}
	id := profile.ChannelIDs[0]
	project := key.Edges.Project.GetActiveProfile()
	if project != nil && len(project.ChannelIDs) > 0 && !slices.Contains(project.ChannelIDs, id) {
		return 0, false
	}
	return id, true
}

func sanitizedProbeWindows(data map[string]any) []probeQuotaWindow {
	windows := make([]probeQuotaWindow, 0)
	// Read the existing normalized snapshot, never its raw provider payload.
	encoded, ok := data["_limits"].([]any)
	if !ok {
		return windows
	}
	for _, item := range encoded {
		limit, ok := item.(map[string]any)
		if !ok || limit["type"] != string(provider_quota.QuotaLimitTypeToken) {
			continue
		}
		window, _ := limit["window"].(string)
		if window != "5h" && window != "7d" && window != "weekly" {
			continue
		}
		value, _ := limit["nextResetAt"].(string)
		resetAt, err := time.Parse(time.RFC3339, value)
		if err != nil {
			continue
		}
		windows = append(windows, probeQuotaWindow{Window: window, NextResetAt: lo.ToPtr(resetAt)})
	}
	return windows
}

func (h *ProbeQuotaHandlers) Get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	key, ok := contexts.GetAPIKey(c.Request.Context())
	id, pinned := pinnedProbeChannel(key)
	if !ok || !pinned {
		JSONError(c, http.StatusForbidden, errors.New("quota polling requires an API key profile pinned to exactly one permitted channel"))
		return
	}
	// API key channel permissions were resolved above. Bypass only for this
	// exact ID projection; do not expose a channel entity or raw quota JSON.
	ctx := authz.WithSystemBypass(c.Request.Context(), "read-pinned-channel-reset-times")
	ch, err := h.params.Ent.Channel.Query().Where(channel.IDEQ(id)).
		Select(channel.FieldID, channel.FieldTags, channel.FieldStatus).Only(ctx)
	if err != nil || ch.Status == channel.StatusArchived {
		JSONError(c, http.StatusNotFound, errors.New("quota snapshot unavailable"))
		return
	}
	if !key.GetActiveProfile().MatchChannelTags(ch.Tags) || !key.Edges.Project.GetActiveProfile().MatchChannelTags(ch.Tags) {
		JSONError(c, http.StatusForbidden, errors.New("channel excluded by active profile tags"))
		return
	}
	snapshot, err := h.params.Ent.ProviderQuotaStatus.Query().
		Where(providerquotastatus.ChannelIDEQ(id)).Only(ctx)
	if err != nil {
		JSONError(c, http.StatusNotFound, errors.New("quota snapshot unavailable; enable provider quota collection"))
		return
	}
	enabled, err := h.params.SystemService.IsProviderQuotaCollectionEnabled(ctx, snapshot.ProviderType.String())
	if err != nil || !enabled {
		JSONError(c, http.StatusConflict, errors.New("provider quota collection is disabled or unavailable"))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"channel_id":  id,
		"observed_at": snapshot.UpdatedAt,
		"server_time": time.Now().UTC(),
		"windows":     sanitizedProbeWindows(snapshot.QuotaData),
	})
}
