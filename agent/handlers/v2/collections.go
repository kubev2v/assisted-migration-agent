package v2

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
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

func (h *Handler) DeleteCollections(c *gin.Context) {
	if err := h.svc.DeleteData(c.Request.Context()); err != nil {
		if srvErrors.IsUpdateInProgressError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}

	c.Status(http.StatusNoContent)
}
