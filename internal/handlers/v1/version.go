package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
)

// GetVersion returns the agent version information
// (GET /version)
func (h *Handler) GetVersion(c *gin.Context) {
	c.JSON(http.StatusOK, v1.VersionInfo{
		Version:     h.cfg.Agent.Version,
		GitCommit:   h.cfg.Agent.GitCommit,
		UiGitCommit: h.cfg.Agent.UIGitCommit,
	})
}
