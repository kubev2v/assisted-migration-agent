package v2

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
)

// ListCollections returns all collections.
// (GET /collections)
func (h *Handler) ListCollections(c *gin.Context) {
	databases := h.svc.CollectionService().List()

	resp := v2.CollectionListResponse{
		Collections: make([]v2.Collection, 0, len(databases)),
	}
	for _, db := range databases {
		resp.Collections = append(resp.Collections, v2.NewCollectionFromDatabase(db))
	}
	c.JSON(http.StatusOK, resp)
}

// DeleteCollection deletes a collection by ID.
// (DELETE /collections/{id})
func (h *Handler) DeleteCollection(c *gin.Context, id string) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}
