package handlers_test

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/handlers"
)

var _ = Describe("PostVddk", func() {
	var (
		tempDir string
		handler *handlers.Handler
		router  *gin.Engine
	)

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "vddk-test")
		Expect(err).NotTo(HaveOccurred())

		gin.SetMode(gin.TestMode)
		handler = handlers.New(tempDir, nil, nil, nil, nil, nil)
		router = gin.New()
		router.POST("/vddk", handler.PostVddk)
	})

	AfterEach(func() {
		os.RemoveAll(tempDir)
	})

	// Given a valid VDDK tarball content
	// When we upload the file
	// Then it should save the file and return bytes and md5 hash
	It("should upload file successfully and return bytes and md5", func() {
		// Arrange
		content := []byte("test vddk tarball content")
		expectedMD5 := md5.Sum(content)

		req := httptest.NewRequest(http.MethodPost, "/vddk", bytes.NewReader(content))
		req.Header.Set("Content-Type", "application/octet-stream")
		w := httptest.NewRecorder()

		// Act
		router.ServeHTTP(w, req)

		// Assert
		Expect(w.Code).To(Equal(http.StatusOK))

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		Expect(err).NotTo(HaveOccurred())

		Expect(response["bytes"]).To(BeNumerically("==", len(content)))
		Expect(response["md5"]).To(Equal(hex.EncodeToString(expectedMD5[:])))

		savedPath := filepath.Join(tempDir, "vddk.tar.gz")
		savedContent, err := os.ReadFile(savedPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(savedContent).To(Equal(content))
	})

	// Given a file larger than 64MB
	// When we try to upload the file
	// Then it should return 413 Request Entity Too Large
	It("should return 413 when file exceeds 64MB", func() {
		// Arrange
		largeContent := strings.NewReader(strings.Repeat("x", 64<<20+1))

		req := httptest.NewRequest(http.MethodPost, "/vddk", largeContent)
		req.Header.Set("Content-Type", "application/octet-stream")
		w := httptest.NewRecorder()

		// Act
		router.ServeHTTP(w, req)

		// Assert
		Expect(w.Code).To(Equal(http.StatusRequestEntityTooLarge))

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		Expect(err).NotTo(HaveOccurred())
		Expect(response["error"]).To(ContainSubstring("64MB"))
	})

	// Given a non-existent data directory
	// When we try to upload a file
	// Then it should return 500 Internal Server Error
	It("should return 500 when dataDir does not exist", func() {
		// Arrange
		nonExistentDir := filepath.Join(tempDir, "nonexistent")
		handler = handlers.New(nonExistentDir, nil, nil, nil, nil, nil)
		router = gin.New()
		router.POST("/vddk", handler.PostVddk)

		content := []byte("test content")
		req := httptest.NewRequest(http.MethodPost, "/vddk", bytes.NewReader(content))
		req.Header.Set("Content-Type", "application/octet-stream")
		w := httptest.NewRecorder()

		// Act
		router.ServeHTTP(w, req)

		// Assert
		Expect(w.Code).To(Equal(http.StatusInternalServerError))

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		Expect(err).NotTo(HaveOccurred())
		Expect(response["error"]).To(ContainSubstring("failed to create file"))
	})

	// Given an empty request body
	// When we upload the file
	// Then it should succeed with zero bytes
	It("should handle empty request body", func() {
		// Arrange
		req := httptest.NewRequest(http.MethodPost, "/vddk", nil)
		req.Header.Set("Content-Type", "application/octet-stream")
		w := httptest.NewRecorder()

		// Act
		router.ServeHTTP(w, req)

		// Assert
		Expect(w.Code).To(Equal(http.StatusOK))

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		Expect(err).NotTo(HaveOccurred())
		Expect(response["bytes"]).To(BeNumerically("==", 0))
	})
})
