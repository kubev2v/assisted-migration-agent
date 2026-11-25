package v2

import (
	"context"
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
	var resp v2.AgentStatus
	resp.FromModel(h.agentStatus(c.Request.Context()))
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

	var resp v2.AgentStatus
	resp.FromModel(h.agentStatus(c.Request.Context()))

	c.JSON(http.StatusOK, resp)
}

func (h *Handler) agentStatus(ctx context.Context) models.AgentStatus {
	status := models.AgentStatus{Console: h.svc.ConsoleService().Status()}
	// Agent status gates UI startup; an image upgrade error must not block it.
	message := "image upgrade status is unavailable"
	status.ImageUpgradeStatus = models.ImageUpgradeStatus{
		CurrentVersion: h.cfg.Agent.Version,
		Upload:         models.ImageUploadStatus{State: "unknown", LastError: &message},
		UpgradeError:   &message,
	}
	imageUpgrade := h.svc.ImageUpgradeService()
	if imageUpgrade != nil {
		imageUpgradeStatus, err := imageUpgrade.Status(ctx)
		if err != nil {
			zap.S().Errorw("reading image upgrade status", "error", err)
			if imageUpgradeStatus.Upload.State == "" {
				return status
			}
			imageUpgradeStatus.Upgrade = nil
			message := "host image upgrade result is unavailable"
			imageUpgradeStatus.UpgradeError = &message
		}
		status.ImageUpgradeStatus = imageUpgradeStatus
	}
	return status
}
