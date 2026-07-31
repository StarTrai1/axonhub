package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/scopes"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm/pipeline"
)

// RequestSwitchHandlersParams contains dependencies for request switching.
type RequestSwitchHandlersParams struct {
	fx.In

	RequestService *biz.RequestService
}

// RequestSwitchHandlers serves the active-request channel switch endpoint.
type RequestSwitchHandlers struct {
	RequestService *biz.RequestService
}

// NewRequestSwitchHandlers creates request switch handlers.
func NewRequestSwitchHandlers(params RequestSwitchHandlersParams) *RequestSwitchHandlers {
	return &RequestSwitchHandlers{RequestService: params.RequestService}
}

// SwitchChannel cancels the active upstream attempt before response output is committed.
func (h *RequestSwitchHandlers) SwitchChannel(c *gin.Context) {
	ctx := c.Request.Context()
	if err := authz.RequireScope(ctx, scopes.ScopeWriteRequests); err != nil {
		JSONError(c, http.StatusForbidden, errors.New("permission denied: requires write_requests scope"))
		return
	}

	projectID, ok := contexts.GetProjectID(ctx)
	if !ok || projectID <= 0 {
		JSONError(c, http.StatusBadRequest, errors.New("Project ID not found in context"))
		return
	}

	var uri DownloadContentRequest
	if err := c.ShouldBindUri(&uri); err != nil {
		JSONError(c, http.StatusBadRequest, fmt.Errorf("Invalid request ID: %w", err))
		return
	}

	req, err := ent.FromContext(ctx).Request.Get(ctx, uri.RequestID)
	if err != nil {
		if ent.IsNotFound(err) {
			JSONError(c, http.StatusNotFound, errors.New("Request not found"))
			return
		}
		JSONError(c, http.StatusInternalServerError, errors.New("Failed to load request"))
		return
	}

	if req.ProjectID != projectID {
		JSONError(c, http.StatusNotFound, errors.New("Request not found"))
		return
	}
	if req.Status != request.StatusProcessing {
		JSONError(c, http.StatusConflict, errors.New("only a processing request can switch channels"))
		return
	}

	err = h.RequestService.RequestSwitchRegistry.Switch(req.ID)
	if err != nil {
		switch {
		case errors.Is(err, pipeline.ErrManualSwitchClosed),
			errors.Is(err, pipeline.ErrManualSwitchNotReady),
			errors.Is(err, pipeline.ErrManualSwitchCommitted),
			errors.Is(err, pipeline.ErrManualSwitchInProgress),
			errors.Is(err, pipeline.ErrManualSwitchNoAlternative):
			JSONError(c, http.StatusConflict, err)
		default:
			JSONError(c, http.StatusInternalServerError, errors.New("Failed to switch request channel"))
		}
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"status": "switching"})
}
