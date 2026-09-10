package v2

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// respondV2VAcquireError writes the standard error response when acquiring the
// v2v inspector service fails. collectionMsg is the 400 body for the
// collection-not-found case (it differs per endpoint). Returns true if it wrote
// a response, so callers can `if respondV2VAcquireError(...) { return }`.
func respondV2VAcquireError(c *gin.Context, err error, collectionMsg string) bool {
	if err == nil {
		return false
	}
	switch {
	case srvErrors.IsCollectionNotFoundError(err):
		c.JSON(http.StatusBadRequest, gin.H{"error": collectionMsg})
	case srvErrors.IsOperationInProgressError(err):
		c.JSON(http.StatusConflict, gin.H{"error": "a report is currently in progress; please wait for it to complete before using v2v inspection"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
	return true
}

// StartV2VInspection starts v2v deep inspection for VMs.
// (POST /inspector/v2v)
func (h *Handler) StartV2VInspection(c *gin.Context) {
	v2vSvc, err := h.svc.V2VInspectorService()
	if respondV2VAcquireError(c, err, "failed to start v2v inspector. You must collect data before starting the inspector") {
		return
	}

	var req v2.StartInspectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	if len(req.VmIds) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "please select at least one virtual machine to run v2v inspection"})
		return
	}

	vddkSvc := h.svc.VddkService()
	if _, err := vddkSvc.Status(c.Request.Context()); err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "a VDDK must be uploaded before starting a v2v inspection"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := v2vSvc.Start(c.Request.Context(), req.VmIds); err != nil {
		if srvErrors.IsOperationInProgressError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if srvErrors.IsCredentialsNotSetError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "credentials required: store via PUT /credentials first"})
			return
		}
		if srvErrors.IsInspectionLimitReachedError(err) || srvErrors.IsVCenterError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, v2.InspectorStatus{State: v2.InspectorStatusStateRunning})
}

// GetV2VInspectorStatus returns the v2v inspector status.
// (GET /inspector/v2v/status)
func (h *Handler) GetV2VInspectorStatus(c *gin.Context, params v2.GetV2VInspectorStatusParams) {
	v2vSvc, err := h.svc.V2VInspectorService()
	if respondV2VAcquireError(c, err, "collect data before using the v2v inspector") {
		return
	}

	status := v2vSvc.GetStatus()

	apiStatus := v2.NewInspectorStatusFromModel(status)

	if params.IncludeVddk != nil && *params.IncludeVddk {
		vddkSvc := h.svc.VddkService()
		s, err := vddkSvc.Status(c.Request.Context())
		if err != nil {
			if !srvErrors.IsResourceNotFoundError(err) {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		} else {
			vddk := v2.VddkProperties{Version: s.Version, Md5: s.Md5}
			apiStatus.Vddk = &vddk
		}
	}

	c.JSON(http.StatusOK, apiStatus)
}

// StopV2VInspection stops the v2v inspector.
// (DELETE /inspector/v2v)
func (h *Handler) StopV2VInspection(c *gin.Context) {
	v2vSvc, err := h.svc.V2VInspectorService()
	if respondV2VAcquireError(c, err, "collect data before using the v2v inspector") {
		return
	}

	if err := v2vSvc.Stop(); err != nil {
		if srvErrors.IsInspectorNotRunningError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v2.NewInspectorStatusFromModel(v2vSvc.GetStatus()))
}

// CancelV2VInspection cancels the in-progress v2v inspection for a single VM.
// (DELETE /inspector/v2v/{vmId})
func (h *Handler) CancelV2VInspection(c *gin.Context, vmId string) {
	v2vSvc, err := h.svc.V2VInspectorService()
	if respondV2VAcquireError(c, err, "collect data before using the v2v inspector") {
		return
	}

	if err := v2vSvc.Cancel(vmId); err != nil {
		if srvErrors.IsInspectorNotRunningError(err) || srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v2.NewInspectorStatusFromModel(v2vSvc.GetStatus()))
}
