package v1

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const (
	MaxVDDKSize = 64 << 20 // 64Mb
)

// StartInspection starts inspection for VMs
// (POST /inspector)
func (h *Handler) StartInspection(c *gin.Context) {
	var req v1.StartInspectionJSONRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	if len(req.VmIds) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Please select at least one virtual machine to run deep inspection."})
		return
	}

	if _, err := h.vddkSrv.Status(c.Request.Context()); err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "A VDDK must be uploaded before starting an inspection"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if req.Credentials != nil {
		creds, err := v1.CredsFromAPI(*req.Credentials)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := creds.Validate(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if _, err := h.credentialsSrv.Store(c.Request.Context(), creds); err != nil {
			if srvErrors.IsVCenterError(err) || srvErrors.IsValidationError(err) {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to store credentials"})
			return
		}
	}

	if err := h.inspectorSrv.Start(c.Request.Context(), req.VmIds); err != nil {
		if srvErrors.IsOperationInProgressError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		if srvErrors.IsCredentialsNotSetError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "credentials required: provide inline or store via PUT /credentials"})
			return
		}
		if srvErrors.IsInspectionLimitReachedError(err) || srvErrors.IsVCenterError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to start inspector: %v", err)})
		return
	}

	c.JSON(http.StatusAccepted, v1.InspectorStatus{State: v1.InspectorStatusStateRunning})
}

// GetInspectorStatus returns the inspector status
// (GET /inspector)
func (h *Handler) GetInspectorStatus(c *gin.Context, params v1.GetInspectorStatusParams) {
	inspS := h.inspectorSrv.GetStatus()

	apiStatus := v1.NewInspectorStatus(inspS)

	if params.IncludeVddk != nil && *params.IncludeVddk && h.vddkSrv != nil {
		s, err := h.vddkSrv.Status(c.Request.Context())
		if err != nil {
			if !srvErrors.IsResourceNotFoundError(err) {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
		} else {
			apiStatus = apiStatus.WithVddk(s)
		}
	}

	c.JSON(http.StatusOK, apiStatus)
}

// StopInspection stops inspector entirely
// (DELETE /inspector)
func (h *Handler) StopInspection(c *gin.Context) {
	if err := h.inspectorSrv.Stop(); err != nil {
		if srvErrors.IsInspectorNotRunningError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, v1.NewInspectorStatus(h.inspectorSrv.GetStatus()))
}

// ValidateInspectorCredentials validates vCenter credentials used by the inspector.
// (POST /inspector/credentials)
func (h *Handler) ValidateInspectorCredentials(c *gin.Context) {
	var req v1.VcenterCredentials
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	creds, err := v1.CredsFromAPI(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := creds.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.inspectorSrv.Credentials(c.Request.Context(), creds); err != nil {
		if srvErrors.IsVCenterError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusOK)
}

// PutInspectorVddk (PUT /inspector/vddk)
func (h *Handler) PutInspectorVddk(c *gin.Context) {
	if h.inspectorSrv != nil && h.inspectorSrv.IsBusy() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "VDDK upload is not allowed while inspector is running"})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, MaxVDDKSize)
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
	defer func() {
		_ = r.Close()
	}()

	s, err := h.vddkSrv.Upload(c.Request.Context(), file.Filename, r)
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

	c.JSON(http.StatusOK, &v1.VddkProperties{
		Version: s.Version,
		Bytes:   &file.Size,
		Md5:     s.Md5,
	})
}

// GetInspectorVddkStatus returns VDDK upload metadata (GET /inspector/vddk).
func (h *Handler) GetInspectorVddkStatus(c *gin.Context) {
	s, err := h.vddkSrv.Status(c.Request.Context())
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, &v1.VddkProperties{
		Version: s.Version,
		Md5:     s.Md5,
	})
}
