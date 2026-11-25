package v2

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/services"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const maxImageUpgradeMultipartOverhead int64 = 1 << 20

func (h *Handler) PostImageUpgrade(c *gin.Context) {
	imageUpgrade := h.svc.ImageUpgradeService()
	if imageUpgrade == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "image upgrade service is unavailable"})
		return
	}
	body := &imageUpgradeBodyCounter{Reader: http.MaxBytesReader(c.Writer, c.Request.Body, services.MaxImageUpgradeImageSize+maxImageUpgradeMultipartOverhead)}
	c.Request.Body = struct {
		io.Reader
		io.Closer
	}{Reader: body, Closer: c.Request.Body}
	mr, err := c.Request.MultipartReader()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected multipart/form-data"})
		return
	}
	part, err := mr.NextRawPart()
	if err != nil {
		writeImageUpgradeError(c, imageUpgradeMultipartError(err))
		return
	}
	if body.n > maxImageUpgradeMultipartOverhead {
		writeImageUpgradeError(c, srvErrors.NewImageUpgradePayloadTooLargeError())
		return
	}
	if part.FormName() != "file" || part.FileName() == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "exactly one file part named file is required"})
		return
	}
	if encoding := part.Header.Get("Content-Transfer-Encoding"); encoding != "" && encoding != "binary" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "encoded file parts are not supported"})
		return
	}
	r := &imageUpgradeMultipartReader{part: part, multipart: mr, body: body}
	if _, err := imageUpgrade.Upload(c.Request.Context(), r); err != nil {
		writeImageUpgradeError(c, err)
		return
	}
	c.Status(http.StatusAccepted)
}

type imageUpgradeBodyCounter struct {
	io.Reader
	n int64
}

func (r *imageUpgradeBodyCounter) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.n += int64(n)
	return n, err
}

// Validate the entire multipart envelope before Upload publishes the archive.
type imageUpgradeMultipartReader struct {
	part      *multipart.Part
	multipart *multipart.Reader
	body      *imageUpgradeBodyCounter
	payload   int64
	done      bool
}

func (r *imageUpgradeMultipartReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	n, err := r.part.Read(p)
	r.payload += int64(n)
	if r.body.n-r.payload > maxImageUpgradeMultipartOverhead {
		return n, srvErrors.NewImageUpgradePayloadTooLargeError()
	}
	if err == io.EOF {
		if _, nextErr := r.multipart.NextRawPart(); nextErr != io.EOF {
			if nextErr == nil {
				return n, srvErrors.NewInvalidImageUpgradeImageError("exactly one file part is required")
			}
			return n, imageUpgradeMultipartError(nextErr)
		}
		// Consume any epilogue too, so trailing data cannot bypass the framing limit.
		if _, drainErr := io.Copy(io.Discard, r.body); drainErr != nil {
			return n, imageUpgradeMultipartError(drainErr)
		}
		if r.body.n-r.payload > maxImageUpgradeMultipartOverhead {
			return n, srvErrors.NewImageUpgradePayloadTooLargeError()
		}
		r.done = true
		return n, io.EOF
	}
	if err != nil {
		return n, imageUpgradeMultipartError(err)
	}
	return n, nil
}

func imageUpgradeMultipartError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		return srvErrors.NewImageUpgradePayloadTooLargeError()
	}
	return srvErrors.NewInvalidImageUpgradeImageError("invalid multipart upload: " + err.Error())
}

func writeImageUpgradeError(c *gin.Context, err error) {
	code := http.StatusInternalServerError
	message := err.Error()

	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		code = http.StatusServiceUnavailable
		message = "image upload was interrupted"
	case srvErrors.IsModeConflictError(err), srvErrors.IsOperationInProgressError(err):
		code = http.StatusConflict
	case srvErrors.IsInvalidImageUpgradeImageError(err):
		code = http.StatusBadRequest
	case srvErrors.IsImageUpgradePayloadTooLargeError(err):
		code = http.StatusRequestEntityTooLarge
	case srvErrors.IsImageUpgradeInsufficientStorageError(err):
		code = http.StatusInsufficientStorage
	}
	if code == http.StatusInternalServerError {
		zap.S().Errorw("image upgrade request failed", "error", err)
		message = "internal server error"
	}
	c.JSON(code, gin.H{"error": message})
}
