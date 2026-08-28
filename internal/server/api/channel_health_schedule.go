package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/scopes"
	"github.com/looplj/axonhub/internal/server/orchestrator"
)

type ChannelHealthScheduleHandlersParams struct {
	fx.In

	Service *orchestrator.ScheduledChannelTestService
}

type ChannelHealthScheduleHandlers struct {
	Service *orchestrator.ScheduledChannelTestService
}

func NewChannelHealthScheduleHandlers(params ChannelHealthScheduleHandlersParams) *ChannelHealthScheduleHandlers {
	return &ChannelHealthScheduleHandlers{Service: params.Service}
}

type channelHealthScheduleURI struct {
	ChannelID int `uri:"channel_id" binding:"required"`
}

type updateChannelHealthSchedulesRequest struct {
	Times []string `json:"times"`
}

func (h *ChannelHealthScheduleHandlers) GetSchedules(c *gin.Context) {
	ctx := c.Request.Context()
	if err := authz.RequireScope(ctx, scopes.ScopeReadChannels); err != nil {
		JSONError(c, http.StatusForbidden, errors.New("permission denied: requires read_channels scope"))
		return
	}

	var uri channelHealthScheduleURI
	if err := c.ShouldBindUri(&uri); err != nil {
		JSONError(c, http.StatusBadRequest, fmt.Errorf("invalid channel ID: %w", err))
		return
	}

	times, err := h.Service.GetSchedules(ctx, uri.ChannelID)
	if err != nil {
		if ent.IsNotFound(err) {
			JSONError(c, http.StatusNotFound, errors.New("channel not found"))
			return
		}
		JSONError(c, http.StatusInternalServerError, errors.New("failed to load scheduled health checks"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"times":    times,
		"timezone": serverTimezoneLabel(),
	})
}

func (h *ChannelHealthScheduleHandlers) UpdateSchedules(c *gin.Context) {
	ctx := c.Request.Context()
	if err := authz.RequireScope(ctx, scopes.ScopeWriteChannels); err != nil {
		JSONError(c, http.StatusForbidden, errors.New("permission denied: requires write_channels scope"))
		return
	}

	var uri channelHealthScheduleURI
	if err := c.ShouldBindUri(&uri); err != nil {
		JSONError(c, http.StatusBadRequest, fmt.Errorf("invalid channel ID: %w", err))
		return
	}
	var input updateChannelHealthSchedulesRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		JSONError(c, http.StatusBadRequest, fmt.Errorf("invalid schedule: %w", err))
		return
	}

	times, err := h.Service.UpdateSchedules(ctx, uri.ChannelID, input.Times)
	if err != nil {
		if ent.IsNotFound(err) {
			JSONError(c, http.StatusNotFound, errors.New("channel not found"))
			return
		}
		JSONError(c, http.StatusBadRequest, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"times":    times,
		"timezone": serverTimezoneLabel(),
	})
}

func (h *ChannelHealthScheduleHandlers) GetResults(c *gin.Context) {
	ctx := c.Request.Context()
	if err := authz.RequireScope(ctx, scopes.ScopeReadChannels); err != nil {
		JSONError(c, http.StatusForbidden, errors.New("permission denied: requires read_channels scope"))
		return
	}

	var after int64
	if value := c.Query("after"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 0 {
			JSONError(c, http.StatusBadRequest, errors.New("invalid result cursor"))
			return
		}
		after = parsed
	}

	results, latest := h.Service.ResultsAfter(after)
	c.JSON(http.StatusOK, gin.H{
		"results":  results,
		"latestID": latest,
	})
}

func serverTimezoneLabel() string {
	name, offset := time.Now().Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}

	return fmt.Sprintf("%s (UTC%s%02d:%02d)", name, sign, offset/3600, offset%3600/60)
}
