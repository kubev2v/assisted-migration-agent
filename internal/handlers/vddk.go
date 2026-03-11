package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const (
	MaxVDDKSize = 64 << 20 // 64Mb
)

// (POST /vddk)
func (h *Handler) PostVddk(c *gin.Context) {
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

	s, err := h.VddkSrv.Upload(file)
	if err != nil {
		if srvErrors.IsResourceInProgressError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, &v1.VddkProperties{
		Version: s.Version,
		Bytes:   s.Bytes,
		Md5:     s.Md5,
	})
}

// (GET /vddk)
func (h *Handler) GetVddkStatus(c *gin.Context) {
	s, err := h.VddkSrv.Status()
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
		Bytes:   s.Bytes,
		Md5:     s.Md5,
	})
}
