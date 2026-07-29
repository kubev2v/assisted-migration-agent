package v2

import (
	"net/http"

	"github.com/gin-gonic/gin"

	v2 "github.com/kubev2v/assisted-migration-agent/api/v2"
)

// CompareCollections handles GET /collections/{aId}/compare/{bId}.
// (GET /collections/{aId}/compare/{bId})
func (h *Handler) CompareCollections(c *gin.Context, aId string, bId string) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}

// CompareCollectionsDiff handles GET /collections/{aId}/compare/{bId}/{dimension}.
// (GET /collections/{aId}/compare/{bId}/{dimension})
func (h *Handler) CompareCollectionsDiff(c *gin.Context, aId string, bId string, dimension v2.CompareCollectionsDiffParamsDimension, params v2.CompareCollectionsDiffParams) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}
