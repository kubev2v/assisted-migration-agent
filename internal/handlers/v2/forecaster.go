package v2

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// StartForecaster starts benchmarking for datastore pairs.
// POST /forecaster
func (h *Handler) StartForecaster(c *gin.Context) {
	var req v2.StartForecasterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	forecastReq := models.ForecastRequest{Pairs: v2.NewDatastorePairsFromAPI(req.Pairs)}
	if req.DiskSizeGb != nil {
		forecastReq.DiskSizeGB = *req.DiskSizeGb
	}
	if req.Iterations != nil {
		forecastReq.Iterations = *req.Iterations
	}
	if req.Concurrency != nil {
		forecastReq.Concurrency = *req.Concurrency
	}

	if err := h.svc.ForecasterService().Start(c.Request.Context(), forecastReq); err != nil {
		if srvErrors.IsOperationInProgressError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if srvErrors.IsCredentialsNotSetError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "credentials required: store via PUT /credentials first"})
			return
		}
		if srvErrors.IsForecasterLimitReachedError(err) || srvErrors.IsValidationError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if srvErrors.IsVCenterError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to start forecaster: %v", err)})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"state": models.ForecasterStateRunning})
}

// GetForecasterStatus returns forecaster status with per-pair details.
// GET /forecaster
func (h *Handler) GetForecasterStatus(c *gin.Context) {
	status := h.svc.ForecasterService().GetStatus()
	c.JSON(http.StatusOK, v2.NewForecasterStatusFromModel(status))
}

// StopForecaster stops the running forecaster.
// DELETE /forecaster
func (h *Handler) StopForecaster(c *gin.Context) {
	if err := h.svc.ForecasterService().Stop(); err != nil {
		if srvErrors.IsForecasterNotRunningError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"state": models.ForecasterStateReady})
}

// GetForecasterRuns returns benchmark runs, optionally filtered by pair name.
// GET /forecaster/runs
func (h *Handler) GetForecasterRuns(c *gin.Context) {
	pairName := c.Query("pairName")

	runs, err := h.svc.ForecasterService().ListRuns(c.Request.Context(), pairName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v2.NewBenchmarkRunsFromModel(runs))
}

// DeleteForecasterRun deletes a specific benchmark run.
// DELETE /forecaster/runs/:id
func (h *Handler) DeleteForecasterRun(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid run ID"})
		return
	}

	if err := h.svc.ForecasterService().DeleteRun(c.Request.Context(), id); err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

// GetForecasterStats returns computed statistics for a pair.
// GET /forecaster/stats
func (h *Handler) GetForecasterStats(c *gin.Context) {
	pairName := c.Query("pairName")
	if pairName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pairName query parameter is required"})
		return
	}

	stats, err := h.svc.ForecasterService().GetStats(c.Request.Context(), pairName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v2.NewForecastStatsFromModel(*stats))
}

// GetForecasterDatastores returns available datastores with storage array info.
// POST /forecaster/datastores
func (h *Handler) GetForecasterDatastores(c *gin.Context) {
	datastores, err := h.svc.ForecasterService().ListDatastores(c.Request.Context())
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no collection available"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v2.NewDatastoreDetailsFromModel(datastores))
}

// PostForecasterPairCapabilities computes offload capabilities for datastore pairs.
// POST /forecaster/capabilities
func (h *Handler) PostForecasterPairCapabilities(c *gin.Context) {
	var req v2.PairCapabilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	caps, err := h.svc.ForecasterService().PairCapabilities(c.Request.Context(), models.PairCapabilityRequest{Pairs: v2.NewDatastorePairsFromAPI(req.Pairs)})
	if err != nil {
		if srvErrors.IsValidationError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v2.NewPairCapabilitiesFromModel(caps))
}

// StopForecasterPair cancels a single pair within the running benchmark.
// DELETE /forecaster/pairs/:name
func (h *Handler) StopForecasterPair(c *gin.Context) {
	pairName := c.Param("name")

	if err := h.svc.ForecasterService().StopPair(pairName); err != nil {
		if srvErrors.IsForecasterNotRunningError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no benchmark is running"})
			return
		}
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("pair %q not found or already finished", pairName)})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"pairName": pairName, "state": "canceled"})
}

// RegisterForecasterRoutes registers forecaster endpoints on the given router group.
func RegisterForecasterRoutes(router *gin.RouterGroup, h *Handler) {
	fg := router.Group("/forecaster")
	fg.POST("", h.StartForecaster)
	fg.GET("", h.GetForecasterStatus)
	fg.DELETE("", h.StopForecaster)
	fg.GET("/runs", h.GetForecasterRuns)
	fg.DELETE("/runs/:id", h.DeleteForecasterRun)
	fg.GET("/stats", h.GetForecasterStats)
	fg.POST("/datastores", h.GetForecasterDatastores)
	fg.POST("/capabilities", h.PostForecasterPairCapabilities)
	fg.DELETE("/pairs/:name", h.StopForecasterPair)
}
