package v2

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
)

// GetVersion returns the agent version information.
// (GET /version)
func (h *Handler) GetVersion(c *gin.Context) {
	c.JSON(http.StatusOK, v2.VersionInfo{
		Version:     h.cfg.Agent.Version,
		GitCommit:   h.cfg.Agent.GitCommit,
		UiGitCommit: h.cfg.Agent.UIGitCommit,
	})
}
