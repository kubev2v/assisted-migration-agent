package v2

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// GetAgentStatus returns the current agent status.
// (GET /agent)
func (h *Handler) GetAgentStatus(c *gin.Context) {
	status := h.svc.ConsoleService().Status()
	var resp v2.AgentStatus
	resp.FromModel(models.AgentStatus{Console: status})

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
