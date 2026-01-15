package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

// GetAgentStatus returns the current agent status
// (GET /agent)
func (h *Handler) GetAgentStatus(c *gin.Context) {
	status := h.consoleSrv.Status()
	var resp v1.AgentStatus
	resp.FromModel(models.AgentStatus{Console: status})

	c.JSON(http.StatusOK, resp)
}

// SetAgentMode changes the agent mode
// (POST /agent)
func (h *Handler) SetAgentMode(c *gin.Context) {
	var req v1.AgentModeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	var mode models.AgentMode
	switch req.Mode {
	case v1.AgentModeRequestModeConnected:
		mode = models.AgentModeConnected
	case v1.AgentModeRequestModeDisconnected:
		mode = models.AgentModeDisconnected
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mode: must be 'connected' or 'disconnected'"})
		return
	}

	if err := h.consoleSrv.SetMode(c.Request.Context(), mode); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to set agent mode"})
		return
	}

	status := h.consoleSrv.Status()
	var resp v1.AgentStatus
	resp.FromModel(models.AgentStatus{Console: status})

	c.JSON(http.StatusOK, resp)
}
