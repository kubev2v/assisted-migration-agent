package v2

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// StartCollector creates and starts a new collector.
// (POST /collectors)
func (h *Handler) StartCollector(c *gin.Context) {
	collector, err := h.svc.CreateCollector()
	if err != nil {
		if srvErrors.IsOperationInProgressError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to start collection. Inspection is in progress"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := collector.Start(c.Request.Context()); err != nil {
		if srvErrors.IsCredentialsNotSetError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "credentials required: store via PUT /credentials first"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, v2.NewCollectorStatus(collector.GetStatus()))
}

// ListCollectors returns all tracked collectors.
// (GET /collectors)
func (h *Handler) ListCollectors(c *gin.Context) {
	collectors := h.svc.ListCollectors()

	resp := v2.CollectorListResponse{
		Collectors: make([]v2.CollectorStatus, 0, len(collectors)),
	}
	for _, svc := range collectors {
		resp.Collectors = append(resp.Collectors, v2.NewCollectorStatus(svc.GetStatus()))
	}
	c.JSON(http.StatusOK, resp)
}

// GetCollectorStatus returns the status of a specific collector.
// (GET /collectors/{id})
func (h *Handler) GetCollectorStatus(c *gin.Context, id string) {
	svc, err := h.svc.GetCollector(id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v2.NewCollectorStatus(svc.GetStatus()))
}

// StopCollector stops and removes a specific collector.
// (DELETE /collectors/{id})
func (h *Handler) StopCollector(c *gin.Context, id string) {
	if err := h.svc.StopCollector(id); err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.Status(http.StatusNoContent)
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}
