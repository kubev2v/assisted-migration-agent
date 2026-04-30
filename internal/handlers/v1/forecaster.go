package v1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	v1api "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// ForecasterService defines the interface for forecaster operations.
type ForecasterService interface {
	Start(ctx context.Context, req models.ForecastRequest) error
	GetStatus() models.ForecasterStatus
	Stop() error
	StopPair(pairName string) error
	IsBusy() bool
	VerifyCredentials(ctx context.Context, creds models.Credentials) error
	DeleteRun(ctx context.Context, runID int64) error
	ListRuns(ctx context.Context, pairName string) ([]models.BenchmarkRun, error)
	GetStats(ctx context.Context, pairName string) (*models.ForecastStats, error)
	ListDatastores(ctx context.Context, creds models.Credentials) ([]models.DatastoreDetail, error)
	PairCapabilities(ctx context.Context, creds models.Credentials, req models.PairCapabilityRequest) ([]models.PairCapability, error)
}

// StartForecaster starts benchmarking for datastore pairs.
// POST /forecaster
func (h *Handler) StartForecaster(c *gin.Context) {
	var req v1api.ForecasterStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	pairs := make([]models.DatastorePair, len(req.Pairs))
	for i, p := range req.Pairs {
		pairs[i] = models.DatastorePair{
			Name:            p.Name,
			SourceDatastore: p.SourceDatastore,
			TargetDatastore: p.TargetDatastore,
		}
		if p.Host != nil {
			pairs[i].Host = *p.Host
		}
	}

	forecastReq := models.ForecastRequest{
		Pairs: pairs,
	}
	if req.Credentials != nil {
		forecastReq.Credentials = models.Credentials{
			URL:      req.Credentials.Url,
			Username: req.Credentials.Username,
			Password: req.Credentials.Password,
		}
	}
	if req.DiskSizeGb != nil {
		forecastReq.DiskSizeGB = *req.DiskSizeGb
	}
	if req.Iterations != nil {
		forecastReq.Iterations = *req.Iterations
	}
	if req.Concurrency != nil {
		forecastReq.Concurrency = *req.Concurrency
	}

	if err := h.forecasterSrv.Start(c.Request.Context(), forecastReq); err != nil {
		if srvErrors.IsOperationInProgressError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if srvErrors.IsCredentialsNotSetError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "credentials required: provide credentials inline"})
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

	c.JSON(http.StatusAccepted, v1api.ForecasterStatus{State: v1api.ForecasterStatusState(models.ForecasterStateRunning)})
}

// GetForecasterStatus returns forecaster status with per-pair details.
// GET /forecaster
func (h *Handler) GetForecasterStatus(c *gin.Context) {
	status := h.forecasterSrv.GetStatus()

	resp := v1api.ForecasterStatus{
		State: v1api.ForecasterStatusState(status.State),
	}

	if len(status.Pairs) > 0 {
		pairs := make([]v1api.ForecasterPairStatus, len(status.Pairs))
		for i, p := range status.Pairs {
			pairs[i] = v1api.ForecasterPairStatus{
				PairName:        p.PairName,
				SourceDatastore: p.SourceDatastore,
				TargetDatastore: p.TargetDatastore,
				State:           v1api.ForecasterPairStatusState(p.State),
				CompletedRuns:   p.CompletedRuns,
				TotalRuns:       p.TotalRuns,
			}
			if p.Host != "" {
				pairs[i].Host = &p.Host
			}
			if p.Error != nil {
				errStr := p.Error.Error()
				pairs[i].Error = &errStr
			}
			if p.PrepBytesTotal > 0 {
				pairs[i].PrepBytesTotal = &p.PrepBytesTotal
			}
			if p.PrepBytesUploaded > 0 {
				pairs[i].PrepBytesUploaded = &p.PrepBytesUploaded
			}
		}
		resp.Pairs = &pairs
	}

	c.JSON(http.StatusOK, resp)
}

// StopForecaster stops the running forecaster.
// DELETE /forecaster
func (h *Handler) StopForecaster(c *gin.Context) {
	if err := h.forecasterSrv.Stop(); err != nil {
		if srvErrors.IsForecasterNotRunningError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, v1api.ForecasterStatus{State: v1api.ForecasterStatusState(models.ForecasterStateReady)})
}

// PutForecasterCredentials validates vCenter credentials (preflight check only).
// PUT /forecaster/credentials
func (h *Handler) PutForecasterCredentials(c *gin.Context) {
	var req v1api.VcenterCredentials
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	creds := models.Credentials{
		URL:      req.Url,
		Username: req.Username,
		Password: req.Password,
	}

	if err := h.forecasterSrv.VerifyCredentials(c.Request.Context(), creds); err != nil {
		if privErr := srvErrors.GetInsufficientPrivilegesError(err); privErr != nil {
			c.JSON(http.StatusForbidden, gin.H{
				"error":             err.Error(),
				"missingPrivileges": privErr.Missing,
			})
			return
		}
		if srvErrors.IsVCenterError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusOK)
}

// GetForecasterRuns returns benchmark runs, optionally filtered by pair name.
// GET /forecaster/runs
func (h *Handler) GetForecasterRuns(c *gin.Context, params v1api.GetForecasterRunsParams) {
	var pairName string
	if params.PairName != nil {
		pairName = *params.PairName
	}

	runs, err := h.forecasterSrv.ListRuns(c.Request.Context(), pairName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result := make([]v1api.BenchmarkRun, len(runs))
	for i, r := range runs {
		result[i] = v1api.BenchmarkRun{
			Id:              r.ID,
			SessionId:       r.SessionID,
			PairName:        r.PairName,
			SourceDatastore: r.SourceDS,
			TargetDatastore: r.TargetDS,
			Iteration:       r.Iteration,
			DiskSizeGb:      r.DiskSizeGB,
			DurationSec:     r.DurationSec,
			ThroughputMbps:  r.ThroughputMBps,
			CreatedAt:       r.CreatedAt,
		}
		if r.PrepDurationSec > 0 {
			result[i].PrepDurationSec = &r.PrepDurationSec
		}
		if r.Method != "" {
			result[i].Method = &r.Method
		}
		if r.Error != "" {
			result[i].Error = &r.Error
		}
	}

	c.JSON(http.StatusOK, result)
}

// DeleteForecasterRun deletes a specific benchmark run.
// DELETE /forecaster/runs/:id
func (h *Handler) DeleteForecasterRun(c *gin.Context, id int64) {
	if err := h.forecasterSrv.DeleteRun(c.Request.Context(), id); err != nil {
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
func (h *Handler) GetForecasterStats(c *gin.Context, params v1api.GetForecasterStatsParams) {
	pairName := params.PairName

	stats, err := h.forecasterSrv.GetStats(c.Request.Context(), pairName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	resp := v1api.ForecastStats{
		PairName:    stats.PairName,
		SampleCount: stats.SampleCount,
	}

	if stats.SampleCount > 0 {
		resp.MeanMbps = &stats.MeanMBps
		resp.MedianMbps = &stats.MedianMBps
		resp.MinMbps = &stats.MinMBps
		resp.MaxMbps = &stats.MaxMBps
		resp.StddevMbps = &stats.StdDevMBps
		resp.Ci95LowerMbps = &stats.CI95Lower
		resp.Ci95UpperMbps = &stats.CI95Upper

		bestCase := stats.EstPer1TB.BestCase.String()
		expected := stats.EstPer1TB.Expected.String()
		worstCase := stats.EstPer1TB.WorstCase.String()
		resp.EstimatePer1TB = &v1api.EstimateRange{
			BestCase:  &bestCase,
			Expected:  &expected,
			WorstCase: &worstCase,
		}
	}

	c.JSON(http.StatusOK, resp)
}

// GetForecasterDatastores returns available datastores with storage array info
// from the forklift-collected inventory. No vSphere credentials are required.
// POST /forecaster/datastores
func (h *Handler) GetForecasterDatastores(c *gin.Context) {
	// Accept optional body for backward compatibility but credentials are ignored.
	var req v1api.ForecasterDatastoresRequest
	_ = c.ShouldBindJSON(&req)

	datastores, err := h.forecasterSrv.ListDatastores(c.Request.Context(), models.Credentials{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result := make([]v1api.DatastoreDetail, len(datastores))
	for i, ds := range datastores {
		result[i] = v1api.DatastoreDetail{
			Name:       ds.Name,
			Type:       ds.Type,
			CapacityGb: ds.CapacityGB,
			FreeGb:     ds.FreeGB,
		}
		if ds.StorageVendor != "" {
			result[i].StorageVendor = &ds.StorageVendor
		}
		if ds.StorageModel != "" {
			result[i].StorageModel = &ds.StorageModel
		}
		if ds.StorageArrayID != "" {
			result[i].StorageArrayId = &ds.StorageArrayID
		}
		if len(ds.NAADevices) > 0 {
			result[i].NaaDevices = &ds.NAADevices
		}
		if len(ds.Capabilities) > 0 {
			caps := make([]v1api.DatastoreDetailCapabilities, len(ds.Capabilities))
			for j, cap := range ds.Capabilities {
				caps[j] = v1api.DatastoreDetailCapabilities(cap)
			}
			result[i].Capabilities = &caps
		}
	}

	c.JSON(http.StatusOK, result)
}

// PostForecasterPairCapabilities computes offload capabilities for datastore pairs
// from the forklift-collected inventory. No vSphere credentials are required.
// POST /forecaster/capabilities
func (h *Handler) PostForecasterPairCapabilities(c *gin.Context) {
	var req v1api.PairCapabilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	pairs := make([]models.DatastorePair, len(req.Pairs))
	for i, p := range req.Pairs {
		pairs[i] = models.DatastorePair{
			Name:            p.Name,
			SourceDatastore: p.SourceDatastore,
			TargetDatastore: p.TargetDatastore,
		}
		if p.Host != nil {
			pairs[i].Host = *p.Host
		}
	}

	caps, err := h.forecasterSrv.PairCapabilities(c.Request.Context(), models.Credentials{}, models.PairCapabilityRequest{Pairs: pairs})
	if err != nil {
		if srvErrors.IsValidationError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result := make([]v1api.PairCapability, len(caps))
	for i, pc := range caps {
		capList := make([]v1api.PairCapabilityCapabilities, len(pc.Capabilities))
		for j, cap := range pc.Capabilities {
			capList[j] = v1api.PairCapabilityCapabilities(cap)
		}
		result[i] = v1api.PairCapability{
			PairName:        pc.PairName,
			SourceDatastore: pc.SourceDatastore,
			TargetDatastore: pc.TargetDatastore,
			Capabilities:    capList,
		}
	}

	c.JSON(http.StatusOK, result)
}

// StopForecasterPair cancels a single pair within the running benchmark.
// DELETE /forecaster/pairs/:name
func (h *Handler) StopForecasterPair(c *gin.Context, name string) {
	pairName := name

	if err := h.forecasterSrv.StopPair(pairName); err != nil {
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
