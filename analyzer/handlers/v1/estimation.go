package analyzer

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	vmfilter "github.com/kubev2v/assisted-migration-agent/internal/filter"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// CalculateMigrationComplexity calculates migration complexity for the latest collection.
// (POST /complexity-estimation)
func (h *Handler) CalculateMigrationComplexity(c *gin.Context) {
	var req v1.MigrationComplexityRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	var expression string
	if req.FilterExpression != nil {
		expression = *req.FilterExpression
		_, err = vmfilter.ParseWithDefaultMap([]byte(expression))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("expression filter is invalid: %v", err)})
			return
		}
	}

	estimationSvc, err := h.svc.LatestEstimationService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result, err := estimationSvc.CalculateMigrationComplexity(c.Request.Context(), expression)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		if srvErrors.IsValidationError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v1.NewMigrationComplexityResponseFromModel(*result))
}
