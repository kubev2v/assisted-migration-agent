package v2

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/services"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const maxUpdateMultipartOverhead int64 = 1 << 20

func (h *Handler) GetUpdateStatus(c *gin.Context) {
	update := h.svc.UpdateService()
	if !h.cfg.Agent.UpdateEnabled || update == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "appliance updates are not enabled"})
		return
	}
	status, err := update.Status(c.Request.Context())
	if err != nil {
		writeUpdateError(c, err)
		return
	}
	c.JSON(http.StatusOK, status)
}

func (h *Handler) PutUpdateImage(c *gin.Context) {
	update := h.svc.UpdateService()
	if !h.cfg.Agent.UpdateEnabled || update == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "appliance updates are not enabled"})
		return
	}
	body := &updateBodyCounter{Reader: http.MaxBytesReader(c.Writer, c.Request.Body, services.MaxUpdateImageSize+maxUpdateMultipartOverhead)}
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
		writeUpdateError(c, updateMultipartError(err))
		return
	}
	if body.n > maxUpdateMultipartOverhead {
		writeUpdateError(c, srvErrors.NewUpdatePayloadTooLargeError())
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
	r := &updateMultipartReader{part: part, multipart: mr, body: body}
	if _, err := update.Prepare(c.Request.Context(), r, -1, ""); err != nil {
		writeUpdateError(c, err)
		return
	}
	status, err := update.Status(c.Request.Context())
	if err != nil {
		writeUpdateError(c, err)
		return
	}
	c.JSON(http.StatusOK, status)
}

type updateBodyCounter struct {
	io.Reader
	n int64
}

func (r *updateBodyCounter) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.n += int64(n)
	return n, err
}

// Validate the entire multipart envelope before Prepare commits the candidate.
type updateMultipartReader struct {
	part      *multipart.Part
	multipart *multipart.Reader
	body      *updateBodyCounter
	payload   int64
	done      bool
}

func (r *updateMultipartReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	n, err := r.part.Read(p)
	r.payload += int64(n)
	if r.body.n-r.payload > maxUpdateMultipartOverhead {
		return n, srvErrors.NewUpdatePayloadTooLargeError()
	}
	if err == io.EOF {
		if _, nextErr := r.multipart.NextRawPart(); nextErr != io.EOF {
			if nextErr == nil {
				return n, srvErrors.NewInvalidUpdateImageError("exactly one file part is required")
			}
			return n, updateMultipartError(nextErr)
		}
		// Consume any epilogue too, so trailing data cannot bypass the framing limit.
		if _, drainErr := io.Copy(io.Discard, r.body); drainErr != nil {
			return n, updateMultipartError(drainErr)
		}
		if r.body.n-r.payload > maxUpdateMultipartOverhead {
			return n, srvErrors.NewUpdatePayloadTooLargeError()
		}
		r.done = true
		return n, io.EOF
	}
	if err != nil {
		return n, updateMultipartError(err)
	}
	return n, nil
}

func updateMultipartError(err error) error {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		return srvErrors.NewUpdatePayloadTooLargeError()
	}
	return srvErrors.NewInvalidUpdateImageError("invalid multipart upload: " + err.Error())
}

func writeUpdateError(c *gin.Context, err error) {
	code := http.StatusInternalServerError
	switch {
	case srvErrors.IsModeConflictError(err), srvErrors.IsOperationInProgressError(err):
		code = http.StatusConflict
	case srvErrors.IsInvalidUpdateImageError(err):
		code = http.StatusBadRequest
	case srvErrors.IsUpdatePayloadTooLargeError(err):
		code = http.StatusRequestEntityTooLarge
	case srvErrors.IsInsufficientStorageError(err):
		code = http.StatusInsufficientStorage
	}
	message := err.Error()
	if code == http.StatusInternalServerError {
		zap.S().Errorw("update request failed", "error", err)
		message = "internal server error"
	}
	c.JSON(code, gin.H{"error": message})
}
