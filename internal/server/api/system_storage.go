package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/looplj/axonhub/internal/scopes"
	"github.com/looplj/axonhub/internal/server/gc"
)

type cleanupPreviewResponse struct {
	Items []gc.CleanupPreviewItem `json:"items"`
}

type cleanupJobResponse struct {
	Job *gc.CleanupJobStatus `json:"job"`
}

func (h *SystemHandlers) PreviewStorageCleanup(c *gin.Context) {
	if !scopes.UserHasScope(c.Request.Context(), scopes.ScopeReadSettings) {
		JSONError(c, http.StatusForbidden, errors.New("permission denied: requires read:settings scope"))
		return
	}

	var input gc.ManualCleanupInput
	if err := c.ShouldBindJSON(&input); err != nil {
		JSONError(c, http.StatusBadRequest, errors.New("invalid storage cleanup request"))
		return
	}

	items, err := h.GCWorker.PreviewManualCleanup(c.Request.Context(), input)
	if err != nil {
		JSONError(c, http.StatusBadRequest, err)
		return
	}

	c.JSON(http.StatusOK, cleanupPreviewResponse{Items: items})
}

func (h *SystemHandlers) StartStorageCleanup(c *gin.Context) {
	if !scopes.UserHasScope(c.Request.Context(), scopes.ScopeWriteSettings) {
		JSONError(c, http.StatusForbidden, errors.New("permission denied: requires write:settings scope"))
		return
	}

	var input gc.ManualCleanupInput
	if err := c.ShouldBindJSON(&input); err != nil {
		JSONError(c, http.StatusBadRequest, errors.New("invalid storage cleanup request"))
		return
	}

	job, err := h.GCWorker.StartManualCleanup(c.Request.Context(), input)
	if err != nil {
		if errors.Is(err, gc.ErrCleanupAlreadyRunning) {
			JSONError(c, http.StatusConflict, err)
			return
		}
		JSONError(c, http.StatusBadRequest, err)
		return
	}

	c.JSON(http.StatusAccepted, cleanupJobResponse{Job: job})
}

func (h *SystemHandlers) GetStorageCleanupJob(c *gin.Context) {
	if !scopes.UserHasScope(c.Request.Context(), scopes.ScopeReadSettings) {
		JSONError(c, http.StatusForbidden, errors.New("permission denied: requires read:settings scope"))
		return
	}

	c.JSON(http.StatusOK, cleanupJobResponse{Job: h.GCWorker.CurrentCleanupJob()})
}
