package v2

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	v2 "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// GetAgentStatus returns the current agent status.
// (GET /agent)
func (h *Handler) GetAgentStatus(c *gin.Context) {
	status := models.AgentStatus{Console: h.svc.ConsoleService().Status(), RVToolsMode: h.cfg.Agent.RVToolsMode}
	// Agent status gates UI startup; an update error must not block it.
	message := "update status is unavailable"
	status.UpdateStatus = models.UpdateStatus{CurrentVersion: h.cfg.Agent.Version, State: "unknown", LastError: &message}
	update := h.svc.UpdateService()
	if update != nil {
		updateStatus, err := update.Status(c.Request.Context())
		if err != nil {
			zap.S().Errorw("reading update status", "error", err)
		} else {
			status.UpdateStatus = updateStatus
		}
	}
	var resp v2.AgentStatus
	resp.FromModel(status)
	c.JSON(http.StatusOK, resp)
}

// SetAgentMode changes the agent mode.
// (POST /agent)
func (h *Handler) SetAgentMode(c *gin.Context) {
	var req v2.AgentModeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	var mode models.AgentMode
	switch req.Mode {
	case v2.Connected:
		mode = models.AgentModeConnected
	case v2.Disconnected:
		mode = models.AgentModeDisconnected
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mode"})
		return
	}

	if err := h.svc.ConsoleService().SetMode(c.Request.Context(), mode); err != nil {
		if srvErrors.IsModeConflictError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	status := h.svc.ConsoleService().Status()
	var resp v2.AgentStatus
	resp.FromModel(models.AgentStatus{Console: status})

	c.JSON(http.StatusOK, resp)
}
