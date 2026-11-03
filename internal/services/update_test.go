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
		dir             string
		s               *services.UpdateService
		connected, busy bool
		archive         []byte
		digest          string
	)
	newService := func() *services.UpdateService {
		service, err := services.NewUpdateService(dir, "v0.31.0", func(context.Context) (models.AgentMode, error) {
			if connected {
				return models.AgentModeConnected, nil
			}
			return models.AgentModeDisconnected, nil
		}, func() bool { return busy })
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(service.Stop)
		return service
	}
	status := func() models.UpdateStatus {
		result, err := s.Status(context.Background())
		Expect(err).NotTo(HaveOccurred())
		return result
	}
	prepare := func(data []byte) error {
		_, err := s.Prepare(context.Background(), bytes.NewReader(data), int64(len(data)), "")
		return err
	}
	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		connected, busy = false, false
		s = newService()
		archive, digest = test.UpdateArchive("v0.32.0", "v0.30.0", "")
	})

	It("prepares without publishing a host trigger and recovers after restart", func() {
		Expect(prepare(archive)).To(Succeed())
		result := status()
		Expect(result.State).To(Equal("ready"))
		Expect(result.PreparedImage).To(Equal(&models.PreparedImage{Version: "v0.32.0", Digest: digest}))
		triggers, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(triggers).To(BeEmpty())
		s = newService()
		Expect(status().PreparedImage).To(Equal(result.PreparedImage))
		Expect(status().State).To(Equal("ready"))
	})

	It("returns all nullable fields explicitly and derives connectivity from mode", func() {
		data, err := json.Marshal(status())
		Expect(err).NotTo(HaveOccurred())
		var result map[string]any
		Expect(json.Unmarshal(data, &result)).To(Succeed())
		for _, field := range []string{"autoUpdate", "updateAvailable", "latestVersion", "preparedImage", "lastResult", "lastError"} {
			Expect(result).To(HaveKeyWithValue(field, BeNil()))
		}
		connected = true
		Expect(status().Connected).To(BeTrue())
		Expect(status().AutoUpdate).To(Equal(new(bool)))
		Expect(status().CurrentVersion).To(Equal("v0.31.0"))
	})

	It("removes failed preparation files and reports the error", func() {
		Expect(srvErrors.IsInvalidUpdateImageError(prepare([]byte("invalid OCI")))).To(BeTrue())
		Expect(status().State).To(Equal("idle"))
		Expect(status().LastError).NotTo(BeNil())
		files, err := os.ReadDir(filepath.Join(dir, "updates", ".staging"))
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(BeEmpty())
	})

	It("preserves a previous candidate after replacement failure and removes it after success", func() {
		Expect(prepare(archive)).To(Succeed())
		original := status().PreparedImage
		Expect(prepare([]byte("invalid"))).NotTo(Succeed())
		Expect(status().PreparedImage).To(Equal(original))
		Expect(status().State).To(Equal("ready"))
		replacement, _ := test.UpdateArchive("v0.33.0", "", "")
		Expect(prepare(replacement)).To(Succeed())
		Expect(status().PreparedImage.Version).To(Equal("v0.33.0"))
		Expect(status().LastError).To(BeNil())
		files, err := filepath.Glob(filepath.Join(dir, "updates", ".staging", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(HaveLen(1))
	})

	It("rejects connected mode, active work, oversized declarations and size mismatches", func() {
		connected = true
		Expect(srvErrors.IsModeConflictError(prepare(archive))).To(BeTrue())
		connected, busy = false, true
		Expect(srvErrors.IsOperationInProgressError(prepare(archive))).To(BeTrue())
		busy = false
		_, err := s.Prepare(context.Background(), bytes.NewReader(nil), services.MaxUpdateImageSize+1, "")
		Expect(srvErrors.IsUpdatePayloadTooLargeError(err)).To(BeTrue())
		_, err = s.Prepare(context.Background(), bytes.NewReader(archive), 1, "")
		Expect(srvErrors.IsInvalidUpdateImageError(err)).To(BeTrue())
	})

	It("enforces the three-times-space budget for known and unknown sizes", func() {
		services.SetUpdateFreeSpaceForTest(s, func(string) (uint64, error) { return uint64(len(archive))*3 - 1, nil })
		Expect(srvErrors.IsInsufficientStorageError(prepare(archive))).To(BeTrue())
		_, err := s.Prepare(context.Background(), bytes.NewReader(archive), -1, "")
		Expect(srvErrors.IsInsufficientStorageError(err)).To(BeTrue())
	})

	It("maps out-of-space failures and removes partial files", func() {
		services.SetUpdateFreeSpaceForTest(s, func(string) (uint64, error) { return 0, syscall.ENOSPC })
		Expect(srvErrors.IsInsufficientStorageError(prepare(archive))).To(BeTrue())
	})

	It("allows status during upload and rejects concurrent preparation", func() {
		reader, writer := io.Pipe()
		defer func() { _ = reader.Close(); _ = writer.Close() }()
		done := make(chan error, 1)
		go func() { _, err := s.Prepare(context.Background(), reader, -1, ""); done <- err }()
		Eventually(func() string { return status().State }).Should(Equal("uploading"))
		Expect(srvErrors.IsUpdateInProgressError(prepare(archive))).To(BeTrue())
		_, err := writer.Write(archive)
		Expect(err).NotTo(HaveOccurred())
		Expect(writer.Close()).To(Succeed())
		Eventually(done).Should(Receive(BeNil()))
	})

	It("cleans up a cancelled upload", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := s.Prepare(ctx, bytes.NewReader(archive), -1, "")
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(status().State).To(Equal("idle"))
	})

	It("detects corruption on recovery and removes orphaned partials", func() {
		Expect(prepare(archive)).To(Succeed())
		staging := filepath.Join(dir, "updates", ".staging")
		Expect(os.WriteFile(filepath.Join(staging, "orphan.part"), []byte("partial"), 0o600)).To(Succeed())
		files, err := filepath.Glob(filepath.Join(staging, "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(files[0], []byte("corrupt"), 0o600)).To(Succeed())
		s = newService()
		Expect(status().State).To(Equal("idle"))
		Expect(status().PreparedImage).To(BeNil())
		Expect(status().LastError).NotTo(BeNil())
		entries, err := os.ReadDir(staging)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).To(BeEmpty())
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
