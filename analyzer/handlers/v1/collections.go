package analyzer

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
)

// ListCollections returns all collections.
// (GET /collections)
func (h *Handler) ListCollections(c *gin.Context) {
	databases := h.svc.CollectionService().List()

	resp := v1.CollectionListResponse{
		Collections: make([]v1.Collection, 0, len(databases)),
	}
	for _, db := range databases {
		resp.Collections = append(resp.Collections, v1.NewCollectionFromDatabase(db))
	}
	c.JSON(http.StatusOK, resp)
}

func (h *Handler) DeleteCollections(c *gin.Context) {
	if err := h.svc.DeleteCollections(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.Status(http.StatusNoContent)
}
