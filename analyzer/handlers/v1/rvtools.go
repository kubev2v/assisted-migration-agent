package analyzer

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// StartRvtoolsCollector accepts RVTools Excel files and starts a collection pipeline.
func (h *Handler) StartRvtoolsCollector(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid multipart form"})
		return
	}

	files := form.File["files"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "at least one file is required"})
		return
	}

	var savedPaths []string
	for _, fh := range files {
		destPath := filepath.Join(h.cfg.Agent.DataFolder, fmt.Sprintf("rvtools_%d_%s", time.Now().UnixNano(), filepath.Base(fh.Filename)))
		if err := c.SaveUploadedFile(fh, destPath); err != nil {
			for _, p := range savedPaths {
				_ = os.Remove(p)
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save file %s: %v", fh.Filename, err)})
			return
		}
		savedPaths = append(savedPaths, destPath)
	}

	status, err := h.svc.StartRVToolsCollecting(savedPaths)
	if err != nil {
		for _, path := range savedPaths {
			_ = os.Remove(path)
		}
		if srvErrors.IsOperationInProgressError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusAccepted, v1.NewCollectorStatus(status))
}
