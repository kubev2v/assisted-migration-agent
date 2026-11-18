package analyzer

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	vmfilter "github.com/kubev2v/assisted-migration-agent/internal/filter"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// (POST /cluster-requirements)
func (h *Handler) CalculateClusterRequirements(c *gin.Context) {
	var req v1.ClusterRequirementsRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	form := req.ToModel()
	if req.FilterExpression != nil {
		_, err = vmfilter.ParseWithDefaultMap([]byte(form.Expression))
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("expression filter is invalid: %v", err)})
			return
		}
	}

	sizerSvc, err := h.svc.LatestSizerService()
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collections found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result, err := sizerSvc.CalculateClusterRequirements(c.Request.Context(), form)
	if err != nil {
		if srvErrors.IsValidationError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v1.NewClusterRequirementsResponseFromModel(*result))
}
