package services_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("UpdateService", func() {
	var (
		dir       string
		s         *services.UpdateService
		connected bool
		archive   []byte
		digest    string
	)
	newService := func() *services.UpdateService {
		service, err := services.NewUpdateService(dir, "v0.31.0", func(context.Context) (models.AgentMode, error) {
			if connected {
				return models.AgentModeConnected, nil
			}
			return models.AgentModeDisconnected, nil
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(service.Stop)
		return service
	}
	status := func() models.UpdateStatus {
		result, err := s.Status(context.Background())
		Expect(err).NotTo(HaveOccurred())
		return result
	}
	upload := func(data []byte) error {
		_, err := s.Upload(context.Background(), bytes.NewReader(data), int64(len(data)), "")
		return err
	}
	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		connected = false
		s = newService()
		archive, digest = test.UpdateArchive("v0.32.0", "v0.30.0", "")
	})

	It("publishes validated images without managing the host upgrade", func() {
		image, err := s.Upload(context.Background(), bytes.NewReader(archive), int64(len(archive)), digest)
		Expect(err).NotTo(HaveOccurred())
		Expect(image).To(Equal(models.UpdateImage{Version: "v0.32.0", Digest: digest}))
		triggers, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(triggers).To(HaveLen(1))
		data, err := os.ReadFile(triggers[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(Equal(archive))
		Expect(status().State).To(Equal("idle"))
		staged, err := os.ReadDir(filepath.Join(dir, "updates", ".staging"))
		Expect(err).NotTo(HaveOccurred())
		Expect(staged).To(BeEmpty())
		// A host result is not a prerequisite for accepting the next upload.
		Expect(upload(archive)).To(Succeed())
		triggers, err = filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(triggers).To(HaveLen(2))
	})

	It("does not let an unreadable host result prevent uploads or agent startup", func() {
		Expect(os.MkdirAll(filepath.Join(dir, "updates"), 0o750)).To(Succeed())
		path := filepath.Join(dir, "updates", "status.json")
		Expect(os.WriteFile(path, []byte("invalid JSON"), 0o600)).To(Succeed())
		s = newService()
		Expect(upload(archive)).To(Succeed())
		_, err := s.Status(context.Background())
		Expect(err).To(HaveOccurred())
	})

	It("returns nullable status fields explicitly", func() {
		data, err := json.Marshal(status())
		Expect(err).NotTo(HaveOccurred())
		var result map[string]any
		Expect(json.Unmarshal(data, &result)).To(Succeed())
		for _, field := range []string{"lastResult", "lastError"} {
			Expect(result).To(HaveKeyWithValue(field, BeNil()))
		}
		Expect(status().CurrentVersion).To(Equal("v0.31.0"))
	})

	It("removes failed preparation files and reports the error", func() {
		Expect(srvErrors.IsInvalidUpdateImageError(upload([]byte("invalid OCI")))).To(BeTrue())
		Expect(status().State).To(Equal("idle"))
		Expect(status().LastError).NotTo(BeNil())
		files, err := os.ReadDir(filepath.Join(dir, "updates", ".staging"))
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(BeEmpty())
	})

	It("rejects connected mode, oversized declarations and size mismatches", func() {
		connected = true
		Expect(srvErrors.IsModeConflictError(upload(archive))).To(BeTrue())
		connected = false
		_, err := s.Upload(context.Background(), bytes.NewReader(nil), services.MaxUpdateImageSize+1, "")
		Expect(srvErrors.IsUpdatePayloadTooLargeError(err)).To(BeTrue())
		_, err = s.Upload(context.Background(), bytes.NewReader(archive), 1, "")
		Expect(srvErrors.IsInvalidUpdateImageError(err)).To(BeTrue())
	})

	It("enforces the three-times-space budget for known and unknown sizes", func() {
		services.SetUpdateFreeSpaceForTest(s, func(string) (uint64, error) { return uint64(len(archive))*3 - 1, nil })
		Expect(srvErrors.IsInsufficientStorageError(upload(archive))).To(BeTrue())
		_, err := s.Upload(context.Background(), bytes.NewReader(archive), -1, "")
		Expect(srvErrors.IsInsufficientStorageError(err)).To(BeTrue())
	})

	It("maps out-of-space failures and removes partial files", func() {
		services.SetUpdateFreeSpaceForTest(s, func(string) (uint64, error) { return 0, syscall.ENOSPC })
		Expect(srvErrors.IsInsufficientStorageError(upload(archive))).To(BeTrue())
	})

	It("publishes only the complete upload by renaming and rejects concurrent uploads", func() {
		reader, writer := io.Pipe()
		defer func() { _ = reader.Close(); _ = writer.Close() }()
		done := make(chan error, 1)
		go func() { _, err := s.Upload(context.Background(), reader, -1, ""); done <- err }()
		Eventually(func() string { return status().State }).Should(Equal("uploading"))
		Expect(srvErrors.IsUpdateInProgressError(upload(archive))).To(BeTrue())
		_, err := writer.Write(archive[:len(archive)/2])
		Expect(err).NotTo(HaveOccurred())
		triggers, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(triggers).To(BeEmpty())
		parts, err := filepath.Glob(filepath.Join(dir, "updates", ".staging", "*.part"))
		Expect(err).NotTo(HaveOccurred())
		Expect(parts).To(HaveLen(1))
		before, err := os.Stat(parts[0])
		Expect(err).NotTo(HaveOccurred())
		_, err = writer.Write(archive[len(archive)/2:])
		Expect(err).NotTo(HaveOccurred())
		Expect(writer.Close()).To(Succeed())
		Eventually(done).Should(Receive(BeNil()))
		triggers, err = filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(triggers).To(HaveLen(1))
		after, err := os.Stat(triggers[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(os.SameFile(before, after)).To(BeTrue())
		Expect(status().State).To(Equal("idle"))
	})

	It("cleans up a cancelled upload", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := s.Upload(ctx, bytes.NewReader(archive), -1, "")
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(status().State).To(Equal("idle"))
	})

	It("removes abandoned partial uploads on startup without touching published images", func() {
		Expect(upload(archive)).To(Succeed())
		staging := filepath.Join(dir, "updates", ".staging")
		Expect(os.WriteFile(filepath.Join(staging, "orphan.part"), []byte("partial"), 0o600)).To(Succeed())
		published, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		s = newService()
		Expect(status().State).To(Equal("idle"))
		entries, err := os.ReadDir(staging)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(BeEmpty())
		data, err := os.ReadFile(published[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(Equal(archive))
	})

	It("reads old and extended host results afresh, without changing current version", func() {
		Expect(os.MkdirAll(filepath.Join(dir, "updates"), 0o750)).To(Succeed())
		path := filepath.Join(dir, "updates", "status.json")
		Expect(os.WriteFile(path, []byte(`{"file":"v0.30.0.tar","result":"applied","time":"2026-11-02T00:00:00Z"}`), 0o600)).To(Succeed())
		Expect(status().LastResult.Result).To(Equal("applied"))
		Expect(status().CurrentVersion).To(Equal("v0.31.0"))
		Expect(os.WriteFile(path, []byte(`{"file":"v0.32.0.tar","result":"rolled-back","time":"2026-11-03T00:00:00Z","error":"failed","error_stage":"restart","running_image":{"version":"v0.31.0","digest":"sha256:old"},"last_successful_update":{"version":"v0.31.0","digest":"sha256:old","time":"2026-11-02T00:00:00Z"},"future":true}`), 0o600)).To(Succeed())
		Expect(status().LastResult.Result).To(Equal("rolled-back"))
		Expect(status().LastResult.RunningImage.Version).To(Equal("v0.31.0"))
		Expect(status().LastResult.LastSuccessfulUpdate).NotTo(BeNil())
		Expect(os.WriteFile(path, []byte(`{"result":"failed"}`), 0o600)).To(Succeed())
		_, err := s.Status(context.Background())
		Expect(err).To(HaveOccurred())
		Expect(os.WriteFile(path, []byte(`bad JSON`), 0o600)).To(Succeed())
		_, err = s.Status(context.Background())
		Expect(err).To(HaveOccurred())
	})
})
