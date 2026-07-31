package v2

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
)

/*
*

	RVTools handler is just a wrapper around the v2 handler
	to forbid access to services not available in rvtools mode

*
*/
type RVToolsHandler struct {
	*Handler
}

func NewRVToolsHandler(cfg config.Configuration, svc ServiceProvider) *RVToolsHandler {
	return &RVToolsHandler{Handler: NewHandler(cfg, svc)}
}

func rvtoolsNotAvailable(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not available in rvtools mode"})
}

// Blocked endpoints — not available in rvtools mode.

func (h *RVToolsHandler) SetAgentMode(c *gin.Context)              { rvtoolsNotAvailable(c) }
func (h *RVToolsHandler) StartCollector(c *gin.Context)            { rvtoolsNotAvailable(c) }
func (h *RVToolsHandler) PutCredentials(c *gin.Context)            { rvtoolsNotAvailable(c) }
func (h *RVToolsHandler) GetCredentials(c *gin.Context)            { rvtoolsNotAvailable(c) }
func (h *RVToolsHandler) DeleteCredentials(c *gin.Context)         { rvtoolsNotAvailable(c) }
func (h *RVToolsHandler) GetCredentialCapabilities(c *gin.Context) { rvtoolsNotAvailable(c) }
func (h *RVToolsHandler) StartInspection(c *gin.Context)           { rvtoolsNotAvailable(c) }
func (h *RVToolsHandler) StopInspection(c *gin.Context)            { rvtoolsNotAvailable(c) }
func (h *RVToolsHandler) PutInspectorVddk(c *gin.Context)          { rvtoolsNotAvailable(c) }
func (h *RVToolsHandler) GetInspectorStatus(c *gin.Context, _ v2.GetInspectorStatusParams) {
	rvtoolsNotAvailable(c)
}
func (h *RVToolsHandler) GetInspectorVddkStatus(c *gin.Context)     { rvtoolsNotAvailable(c) }
func (h *RVToolsHandler) ListApplications(c *gin.Context, _ string) { rvtoolsNotAvailable(c) }
func (h *RVToolsHandler) CompareCollections(c *gin.Context, _ string, _ string) {
	rvtoolsNotAvailable(c)
}
func (h *RVToolsHandler) CompareCollectionsDiff(c *gin.Context, _ string, _ string, _ v2.CompareCollectionsDiffParamsDimension, _ v2.CompareCollectionsDiffParams) {
	rvtoolsNotAvailable(c)
}
func (h *RVToolsHandler) GetClusterUtilization(c *gin.Context, _ string, _ string) {
	rvtoolsNotAvailable(c)
}
func (h *RVToolsHandler) StartForecaster(c *gin.Context)                { rvtoolsNotAvailable(c) }
func (h *RVToolsHandler) StopForecaster(c *gin.Context)                 { rvtoolsNotAvailable(c) }
func (h *RVToolsHandler) GetForecasterStatus(c *gin.Context)            { rvtoolsNotAvailable(c) }
func (h *RVToolsHandler) PostForecasterPairCapabilities(c *gin.Context) { rvtoolsNotAvailable(c) }
func (h *RVToolsHandler) GetForecasterDatastores(c *gin.Context)        { rvtoolsNotAvailable(c) }
func (h *RVToolsHandler) StopForecasterPair(c *gin.Context, _ string)   { rvtoolsNotAvailable(c) }
func (h *RVToolsHandler) GetForecasterRuns(c *gin.Context, _ v2.GetForecasterRunsParams) {
	rvtoolsNotAvailable(c)
}
func (h *RVToolsHandler) DeleteForecasterRun(c *gin.Context, _ int64) { rvtoolsNotAvailable(c) }
func (h *RVToolsHandler) GetForecasterStats(c *gin.Context, _ v2.GetForecasterStatsParams) {
	rvtoolsNotAvailable(c)
}
