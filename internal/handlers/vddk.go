package handlers

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

const (
	maxVDDKSize  = 64 << 20 // 64Mb
	vddkFilename = "vddk.tar.gz"
)

// (POST /vddk)
func (h *Handler) PostVddk(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxVDDKSize)

	dst, err := os.Create(filepath.Join(h.dataDir, vddkFilename))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create file"})
		return
	}
	defer dst.Close()

	hash := md5.New()
	written, err := io.Copy(io.MultiWriter(dst, hash), c.Request.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file exceeds 64MB"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save file"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"bytes": written,
		"md5":   hex.EncodeToString(hash.Sum(nil)),
	})
}
