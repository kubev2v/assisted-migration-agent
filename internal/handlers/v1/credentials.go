package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

func (h *Handler) PutCredentials(c *gin.Context) {
	var req v1.PutCredentialsJSONRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": validationErrorMessage(err)})
		return
	}

	creds, err := v1.CredsFromAPI(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	url, err := h.credentialsSrv.Store(c.Request.Context(), creds)
	if err != nil {
		if srvErrors.IsVCenterError(err) || srvErrors.IsValidationError(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		zap.S().Errorw("failed to store credentials", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, v1.CredentialStatus{Url: url, Valid: true})
}

func (h *Handler) GetCredentials(c *gin.Context) {
	url, err := h.credentialsSrv.Status(c.Request.Context())
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no credentials stored"})
			return
		}
		zap.S().Errorw("failed to get credentials", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, v1.CredentialStatus{Url: url, Valid: true})
}

func (h *Handler) GetCredentialCapabilities(c *gin.Context) {
	status, err := h.credentialsSrv.GetCapabilities(c.Request.Context())
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no credentials stored"})
			return
		}
		zap.S().Errorw("failed to get capabilities", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusOK, v1.CapabilityStatus{
		Capabilities: struct {
			Collector  v1.OperationCapability `json:"collector"`
			Forecaster v1.OperationCapability `json:"forecaster"`
			Inspector  v1.OperationCapability `json:"inspector"`
		}{
			Collector:  capabilityToAPI(status.Collector),
			Inspector:  capabilityToAPI(status.Inspector),
			Forecaster: capabilityToAPI(status.Forecaster),
		},
	})
}

func capabilityToAPI(c models.OperationCapability) v1.OperationCapability {
	result := v1.OperationCapability{Enabled: c.Enabled}
	if len(c.MissingPrivileges) > 0 {
		result.MissingPrivileges = &c.MissingPrivileges
	}
	return result
}

func (h *Handler) DeleteCredentials(c *gin.Context) {
	if err := h.credentialsSrv.DeleteAll(c.Request.Context()); err != nil {
		zap.S().Errorw("failed to delete credentials", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.Status(http.StatusNoContent)
}
