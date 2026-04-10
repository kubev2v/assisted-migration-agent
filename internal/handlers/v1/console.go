package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/pkg/errors"
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
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	var mode models.AgentMode
	switch req.Mode {
	case v1.AgentModeRequestModeConnected:
		mode = models.AgentModeConnected
	case v1.AgentModeRequestModeDisconnected:
		mode = models.AgentModeDisconnected
	}

	if err := h.consoleSrv.SetMode(c.Request.Context(), mode); err != nil {
		if errors.IsModeConflictError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	status := h.consoleSrv.Status()
	var resp v1.AgentStatus
	resp.FromModel(models.AgentStatus{Console: status})

	c.JSON(http.StatusOK, resp)
}
