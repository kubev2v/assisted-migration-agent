package services_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/services"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// buildMinimalTarGz returns a minimal valid .tar.gz with one file (for extraction tests).
func buildMinimalTarGz(name, content string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if dir := filepath.Dir(name); dir != "." {
		_ = tw.WriteHeader(&tar.Header{Name: dir + "/", Typeflag: tar.TypeDir, Mode: 0755})
	}
	hdr := &tar.Header{Name: name, Mode: 0644, Size: int64(len(content))}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write([]byte(content))
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// multipartFileHeaderFromTarGz builds a multipart form with the given tar.gz and returns the FileHeader.
// The form is parsed by the test HTTP server so the file is backed by a temp file (seekable).
func multipartFileHeaderFromTarGz(filename string, tarGz []byte) (*multipart.FileHeader, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(tarGz); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	req := httptest.NewRequest(http.MethodPost, "/", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	file, header, err := req.FormFile("file")
	if err != nil {
		return nil, err
	}
	_ = file.Close() // release fd; service will use header.Open() to read again
	return header, nil
}

var _ = Describe("VddkService", func() {
	var (
		dataDir string
		srv     *services.VddkService
	)

	BeforeEach(func() {
		var err error
		dataDir, err = os.MkdirTemp("", "vddk-test-*")
		Expect(err).NotTo(HaveOccurred())
		srv = services.NewVddkService(dataDir)
	})

	AfterEach(func() {
		if dataDir != "" {
			_ = os.RemoveAll(dataDir)
		}
	})

	Describe("Upload", func() {
		It("extracts tar.gz, saves status and returns version/bytes/md5", func() {
			tarGz := buildMinimalTarGz("lib/lib64.so", "vddk-library-content")
			header, err := multipartFileHeaderFromTarGz("VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", tarGz)
			Expect(err).NotTo(HaveOccurred())

			status, err := srv.Upload(header)
			Expect(err).NotTo(HaveOccurred())
			Expect(status).NotTo(BeNil())
			Expect(status.Version).To(Equal("8.0.3"))
			Expect(status.Bytes).To(Equal(int64(len(tarGz))))
			Expect(status.Md5).To(HaveLen(32))

			// Extracted content exists
			extracted := filepath.Join(dataDir, "vddk", "lib", "lib64.so")
			data, err := os.ReadFile(extracted)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal("vddk-library-content"))

			// Status is persisted
			st, err := srv.Status()
			Expect(err).NotTo(HaveOccurred())
			Expect(st.Version).To(Equal(status.Version))
			Expect(st.Bytes).To(Equal(status.Bytes))
			Expect(st.Md5).To(Equal(status.Md5))
		})

		It("returns error when file is not a valid tar.gz", func() {
			invalidContent := []byte("not a tar.gz file")
			header, err := multipartFileHeaderFromTarGz("VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", invalidContent)
			Expect(err).NotTo(HaveOccurred())

			status, err := srv.Upload(header)
			Expect(err).To(HaveOccurred())
			Expect(status).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("error extracting vddk"))
		})

		It("does not override previous content when upload is invalid", func() {
			// Upload valid VDDK first
			tarGz := buildMinimalTarGz("lib/lib64.so", "original-vddk-content")
			header, err := multipartFileHeaderFromTarGz("VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", tarGz)
			Expect(err).NotTo(HaveOccurred())

			firstStatus, err := srv.Upload(header)
			Expect(err).NotTo(HaveOccurred())
			Expect(firstStatus).NotTo(BeNil())

			extractedPath := filepath.Join(dataDir, "vddk", "lib", "lib64.so")
			Expect(extractedPath).To(BeARegularFile())
			content, err := os.ReadFile(extractedPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("original-vddk-content"))

			// Attempt upload of bad file
			badHeader, err := multipartFileHeaderFromTarGz("VMware-vix-disklib-9.0.0-bad.x86_64.tar.gz", []byte("not a tar.gz"))
			Expect(err).NotTo(HaveOccurred())

			_, err = srv.Upload(badHeader)
			Expect(err).To(HaveOccurred())

			// Previous extracted content must still be present and unchanged
			Expect(extractedPath).To(BeARegularFile())
			contentAfter, err := os.ReadFile(extractedPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(contentAfter)).To(Equal("original-vddk-content"))

			// Status must still reflect the first successful upload
			st, err := srv.Status()
			Expect(err).NotTo(HaveOccurred())
			Expect(st.Version).To(Equal(firstStatus.Version))
			Expect(st.Bytes).To(Equal(firstStatus.Bytes))
			Expect(st.Md5).To(Equal(firstStatus.Md5))
		})

		It("returns unknown version when filename format is invalid", func() {
			tarGz := buildMinimalTarGz("lib/foo.so", "x")
			header, err := multipartFileHeaderFromTarGz("invalid-name.tar.gz", tarGz)
			Expect(err).NotTo(HaveOccurred())

			status, err := srv.Upload(header)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Version).To(Equal("unknown"))
		})

		It("returns VddkUploadInProgressError when upload is already in progress", func() {
			tarGz := buildMinimalTarGz("slow", "x")
			const concurrency = 4
			headers := make([]*multipart.FileHeader, concurrency)
			for i := 0; i < concurrency; i++ {
				h, err := multipartFileHeaderFromTarGz("VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", tarGz)
				Expect(err).NotTo(HaveOccurred())
				headers[i] = h
			}

			var wg sync.WaitGroup
			results := make([]error, concurrency)
			for i := 0; i < concurrency; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					_, results[idx] = srv.Upload(headers[idx])
				}(i)
			}
			wg.Wait()

			var successCount, inProgressCount int
			for _, err := range results {
				if err == nil {
					successCount++
				} else if srvErrors.IsResourceInProgressError(err) {
					inProgressCount++
				}
			}
			Expect(successCount).To(Equal(1), "exactly one upload should succeed")
			Expect(inProgressCount).To(Equal(concurrency-1), "all other uploads should get in-progress error")
		})
	})

	Describe("Status", func() {
		It("returns VddkNotFoundError when no config exists", func() {
			_, err := srv.Status()
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("returns saved status when config exists", func() {
			// Upload once to create config
			tarGz := buildMinimalTarGz("lib/x.so", "y")
			header, err := multipartFileHeaderFromTarGz("VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", tarGz)
			Expect(err).NotTo(HaveOccurred())
			uploaded, err := srv.Upload(header)
			Expect(err).NotTo(HaveOccurred())

			st, err := srv.Status()
			Expect(err).NotTo(HaveOccurred())
			Expect(st.Version).To(Equal(uploaded.Version))
			Expect(st.Bytes).To(Equal(uploaded.Bytes))
			Expect(st.Md5).To(Equal(uploaded.Md5))
		})
	})

	Describe("extractVersion", func() {
		// extractVersion is unexported; we test via Upload with different filenames
		It("parses version from VMware-vix-disklib-X.Y.Z-... filename", func() {
			tarGz := buildMinimalTarGz("lib/x.so", "z")
			header, err := multipartFileHeaderFromTarGz("VMware-vix-disklib-12.34.56-12345678.x86_64.tar.gz", tarGz)
			Expect(err).NotTo(HaveOccurred())

			status, err := srv.Upload(header)
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Version).To(Equal("12.34.56"))
		})
	})
})
