package analyzer

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
)

func (h *Handler) GetAnalyzerStatus(c *gin.Context) {
	c.JSON(http.StatusOK, v1.AnalyzerStatus{Status: v1.Ready})
}
