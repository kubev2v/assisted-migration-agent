package analyzer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	"go.uber.org/zap"

	v1 "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const maxRVToolsUploadSize = 50 << 20

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

	// Check the complete batch before saving files or starting collection.
	for _, fh := range files {
		if fh.Size == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "rvtools file is empty"})
			return
		}
		if fh.Size > maxRVToolsUploadSize {
			c.JSON(http.StatusBadRequest, gin.H{"error": "file exceeds maximum upload size of 50 MiB"})
			return
		}
		file, err := fh.Open()
		if err != nil {
			zap.S().Errorw("failed to read RVTools upload", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read file"})
			return
		}
		var header [4]byte
		_, readErr := io.ReadFull(file, header[:])
		_ = file.Close()
		if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
			zap.S().Errorw("failed to read RVTools upload", "error", readErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read file"})
			return
		}
		if readErr != nil || !bytes.Equal(header[:], []byte{'P', 'K', 3, 4}) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "file is not a valid Excel file"})
			return
		}
	}

	var savedPaths []string
	for _, fh := range files {
		destPath := filepath.Join(h.cfg.Agent.DataFolder, fmt.Sprintf("rvtools_%d_%s", time.Now().UnixNano(), filepath.Base(fh.Filename)))
		if err := c.SaveUploadedFile(fh, destPath); err != nil {
			for _, p := range savedPaths {
				_ = os.Remove(p)
			}
			zap.S().Errorw("failed to save RVTools upload", "file", destPath, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save file"})
			return
		}
		savedPaths = append(savedPaths, destPath)
	}

	status, err := h.svc.StartRVToolsCollecting(savedPaths)
	if err != nil {
		for _, path := range savedPaths {
			_ = os.Remove(path)
		}
		zap.S().Errorw("failed to start RVTools collection", "error", err)
		var inProgress *srvErrors.OperationInProgressError
		if errors.As(err, &inProgress) {
			c.JSON(http.StatusConflict, gin.H{"error": inProgress.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": duckdb_parser.RVToolsFailureMessage})
		return
	}

	c.JSON(http.StatusAccepted, v1.NewCollectorStatus(status))
}
