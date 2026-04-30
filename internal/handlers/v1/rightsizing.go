package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// ListRightsizingReports returns all stored rightsizing reports.
// (GET /rightsizing)
func (h *Handler) ListRightsizingReports(c *gin.Context) {
	reports, err := h.rightsizingSrv.ListReports(c.Request.Context())
	if err != nil {
		zap.S().Named("rightsizing_handler").Errorw("failed to list reports", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	apiReports := make([]v1.RightsizingReportSummary, 0, len(reports))
	for _, r := range reports {
		apiReports = append(apiReports, v1.NewRightsizingReportSummaryFromModel(r))
	}

	c.JSON(http.StatusOK, v1.RightsizingReportListResponse{
		Reports: apiReports,
		Total:   len(apiReports),
	})
}

// GetRightsizingReport returns a single rightsizing report by ID.
// (GET /rightsizing/{id})
func (h *Handler) GetRightsizingReport(c *gin.Context, id string) {
	report, err := h.rightsizingSrv.GetReport(c.Request.Context(), id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		zap.S().Named("rightsizing_handler").Errorw("failed to get report", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v1.NewRightsizingReportFromModel(*report))
}

// TriggerRightsizingCollection triggers a rightsizing metrics collection run.
// (POST /rightsizing)
func (h *Handler) TriggerRightsizingCollection(c *gin.Context) {
	var req v1.RightsizingCollectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	params := models.RightsizingParams{
		Credentials: models.Credentials{
			URL:      req.Credentials.Url,
			Username: req.Credentials.Username,
			Password: req.Credentials.Password,
		},
		LookbackH:  defaultInt(req.LookbackHours, 720),
		IntervalID: defaultInt(req.IntervalId, 7200),
		BatchSize:  defaultInt(req.BatchSize, 64),
	}
	if req.NameFilter != nil {
		params.NameFilter = *req.NameFilter
	}
	if req.ClusterId != nil {
		params.ClusterID = *req.ClusterId
	}
	if req.DiscoverVms != nil {
		params.DiscoverVMs = *req.DiscoverVms
	}

	report, err := h.rightsizingSrv.TriggerCollection(c.Request.Context(), params)
	if err != nil {
		zap.S().Named("rightsizing_handler").Errorw("failed to trigger collection", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, v1.NewRightsizingReportSummaryFromModel(*report))
}

// GetVMUtilization returns utilization details for a specific VM.
// (GET /vms/{id}/utilization)
func (h *Handler) GetVMUtilization(c *gin.Context, id string) {
	details, err := h.rightsizingSrv.GetVMUtilization(c.Request.Context(), id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		zap.S().Named("rightsizing_handler").Errorw("failed to get VM utilization", "id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, v1.NewVmUtilizationDetailsFromModel(*details))
}

func defaultInt(p *int, fallback int) int {
	if p != nil {
		return *p
	}
	return fallback
}
