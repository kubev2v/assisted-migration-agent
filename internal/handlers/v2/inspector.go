package v2

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const maxVDDKSize = 64 << 20

// StartInspection starts deep inspection for VMs.
// (POST /inspector)
func (h *Handler) StartInspection(c *gin.Context) {
	inspSvc, err := h.svc.InspectorService()
	if err != nil {
		if srvErrors.IsCollectionNotFoundError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to start inspector. You must collect data before starting the inspector"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	var req v2.StartInspectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	if len(req.VmIds) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "please select at least one virtual machine to run deep inspection"})
		return
	}

	vddkSvc := h.svc.VddkService()
	if _, err := vddkSvc.Status(c.Request.Context()); err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "a VDDK must be uploaded before starting an inspection"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := inspSvc.Start(c.Request.Context(), req.VmIds); err != nil {
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

// GetInspectorStatus returns the inspector status.
// (GET /inspector)
func (h *Handler) GetInspectorStatus(c *gin.Context, params v2.GetInspectorStatusParams) {
	inspSvc, err := h.svc.InspectorService()
	if err != nil {
		if srvErrors.IsCollectionNotFoundError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "collect data before using the inspector"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	status := inspSvc.GetStatus()

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

// StopInspection stops the inspector.
// (DELETE /inspector)
func (h *Handler) StopInspection(c *gin.Context) {
	inspSvc, err := h.svc.InspectorService()
	if err != nil {
		if srvErrors.IsCollectionNotFoundError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "collect data before using the inspector"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if err := inspSvc.Stop(); err != nil {
		if srvErrors.IsInspectorNotRunningError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v2.NewInspectorStatusFromModel(inspSvc.GetStatus()))
}

// PutInspectorVddk uploads a VDDK tarball.
// (PUT /inspector/vddk)
func (h *Handler) PutInspectorVddk(c *gin.Context) {
	inspSvc, err := h.svc.InspectorService()
	if err != nil {
		if srvErrors.IsCollectionNotFoundError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "collect data before using the inspector"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if inspSvc.IsBusy() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "VDDK upload is not allowed while inspector is running"})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxVDDKSize)
	file, err := c.FormFile("file")
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	r, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer func() { _ = r.Close() }()

	vddkSvc := h.svc.VddkService()
	s, err := vddkSvc.Upload(c.Request.Context(), file.Filename, r)
	if err != nil {
		if srvErrors.IsInvalidVersionError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if srvErrors.IsOperationInProgressError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v2.VddkProperties{
		Version: s.Version,
		Bytes:   &file.Size,
		Md5:     s.Md5,
	})
}

// GetInspectorVddkStatus returns VDDK upload metadata.
// (GET /inspector/vddk)
func (h *Handler) GetInspectorVddkStatus(c *gin.Context) {
	vddkSvc := h.svc.VddkService()
	s, err := vddkSvc.Status(c.Request.Context())
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, v2.VddkProperties{
		Version: s.Version,
		Md5:     s.Md5,
	})
}

// RemoveVMFromInspection cancels inspection for a specific VM.
// (DELETE /collections/{id}/virtualmachines/{vmId}/inspection)
func (h *Handler) RemoveVMFromInspection(c *gin.Context, id string, vmId string) {
	inspSvc, err := h.svc.InspectorService()
	if err != nil {
		if srvErrors.IsCollectionNotFoundError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "collect data before using the inspector"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	if err := inspSvc.Cancel(vmId); err != nil {
		if srvErrors.IsInspectorNotRunningError(err) {
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
	c.Status(http.StatusNoContent)
}
