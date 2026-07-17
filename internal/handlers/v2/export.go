package v2

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// ExportCollection exports collection data as a CSV ZIP archive.
// (GET /collections/{id}/export)
func (h *Handler) ExportCollection(c *gin.Context, id string, params v2.ExportCollectionParams) {
	exportSvc, err := h.svc.ExportService(id)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "collection not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	scopeParam := "overview"
	if params.Scope != nil {
		scopeParam = *params.Scope
	}

	seen := make(map[string]bool)
	var scopes []string
	for _, scope := range strings.Split(scopeParam, ",") {
		scope = strings.TrimSpace(scope)
		if scope != "" && !seen[scope] {
			seen[scope] = true
			scopes = append(scopes, scope)
		}
	}
	if len(scopes) == 0 {
		scopes = []string{"overview"}
	}

	for _, scope := range scopes {
		if !exportSvc.IsValidScope(scope) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid scope: %s", scope)})
			return
		}
	}

	var buf bytes.Buffer
	if err := exportSvc.WriteZip(c.Request.Context(), scopes, &buf); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "export generation failed"})
		return
	}

	c.Header("Content-Disposition", `attachment; filename="migration-advisor-export.zip"`)
	c.Data(http.StatusOK, "application/zip", buf.Bytes())
}
