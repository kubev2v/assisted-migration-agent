package v1

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	api "github.com/kubev2v/assisted-migration-agent/api/v1"
)

// ExportInventory handles GET /api/v1/export
// Returns ZIP archive containing CSV files for requested scopes.
func (h *Handler) ExportInventory(c *gin.Context, params api.ExportInventoryParams) {
	// Parse scope parameter (default: overview)
	scopeParam := "overview"
	if params.Scope != nil {
		scopeParam = *params.Scope
	}
	scopes := strings.Split(scopeParam, ",")

	// Trim whitespace, filter out empty strings, and deduplicate
	seen := make(map[string]bool)
	filtered := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if scope != "" && !seen[scope] {
			seen[scope] = true
			filtered = append(filtered, scope)
		}
	}
	scopes = filtered

	// Default to overview if filtered list is empty
	if len(scopes) == 0 {
		scopes = []string{"overview"}
	}

	for _, scope := range scopes {
		if !h.exportSrv.IsValidScope(scope) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid scope: %s", scope)})
			return
		}
	}

	var buf bytes.Buffer
	if err := h.exportSrv.WriteZip(c.Request.Context(), scopes, &buf); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "export generation failed"})
		return
	}

	c.Header("Content-Disposition", `attachment; filename="migration-advisor-export.zip"`)
	c.Data(http.StatusOK, "application/zip", buf.Bytes())
}
