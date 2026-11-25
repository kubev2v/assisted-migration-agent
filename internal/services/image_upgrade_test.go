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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ocidigest "github.com/opencontainers/go-digest"
	v1 "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("ImageUpgradeService", func() {
	var (
		dir       string
		s         *services.ImageUpgradeService
		connected bool
		archive   []byte
		digest    string
	)
	newService := func() *services.ImageUpgradeService {
		service, err := services.NewImageUpgradeService(dir, "v0.31.0", func(context.Context) (models.AgentMode, error) {
			if connected {
				return models.AgentModeConnected, nil
			}
			return models.AgentModeDisconnected, nil
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(service.Stop)
		return service
	}
	status := func() models.ImageUpgradeStatus {
		result, err := s.Status(context.Background())
		Expect(err).NotTo(HaveOccurred())
		return result
	}
	upload := func(data []byte) error {
		_, err := s.Upload(context.Background(), bytes.NewReader(data))
		return err
	}
	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		connected = false
		s = newService()
		archive, digest = test.ImageUpgradeArchive("v0.32.0", "v0.30.0", "")
	})

	It("publishes validated images without managing the host upgrade", func() {
		image, err := s.Upload(context.Background(), bytes.NewReader(archive))
		Expect(err).NotTo(HaveOccurred())
		Expect(image).To(Equal(models.ImageUpgradeImage{Version: "v0.32.0", Digest: digest}))
		triggers, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(triggers).To(HaveLen(1))
		data, err := os.ReadFile(triggers[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(Equal(archive))
		Expect(status().Upload.State).To(Equal("idle"))
		staged, err := os.ReadDir(filepath.Join(dir, "updates", ".staging"))
		Expect(err).NotTo(HaveOccurred())
		Expect(staged).To(BeEmpty())
	})

	DescribeTable("rejects uploads until the host resolves the published archive", func(restart, quarantine bool) {
		Expect(upload(archive)).To(Succeed())
		published, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(published).To(HaveLen(1))
		if restart {
			s = newService()
		}
		before := status()
		body := bytes.NewBuffer(archive)
		_, err = s.Upload(context.Background(), body)
		Expect(srvErrors.IsImageUpgradeInProgressError(err)).To(BeTrue())
		Expect(body.Len()).To(Equal(len(archive)))
		Expect(status()).To(Equal(before))
		pending, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(pending).To(Equal(published))
		data, err := os.ReadFile(published[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(Equal(archive))
		staging, err := os.ReadDir(filepath.Join(dir, "updates", ".staging"))
		Expect(err).NotTo(HaveOccurred())
		Expect(staging).To(BeEmpty())

		if quarantine {
			rejected := filepath.Join(dir, "updates", "rejected")
			Expect(os.Mkdir(rejected, 0o750)).To(Succeed())
			Expect(os.Rename(published[0], filepath.Join(rejected, filepath.Base(published[0])))).To(Succeed())
		} else {
			Expect(os.Remove(published[0])).To(Succeed())
		}
		Expect(upload(archive)).To(Succeed())
		pending, err = filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(pending).To(HaveLen(1))
	},
		Entry("same service, host removes archive", false, false),
		Entry("same service, host quarantines archive", false, true),
		Entry("recreated service, host removes archive", true, false),
		Entry("recreated service, host quarantines archive", true, true),
	)

	It("ignores staged archives and partial files when checking for pending upgrades", func() {
		staging := filepath.Join(dir, "updates", ".staging")
		Expect(os.MkdirAll(staging, 0o750)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(staging, "unfinished.tar"), []byte("partial"), 0o600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(dir, "updates", "unfinished.part"), []byte("partial"), 0o600)).To(Succeed())
		Expect(upload(archive)).To(Succeed())
	})

	It("does not read the upload body if checking the pending archive directory fails", func() {
		Expect(os.WriteFile(filepath.Join(dir, "updates"), []byte("not a directory"), 0o600)).To(Succeed())
		body := bytes.NewBuffer(archive)
		_, err := s.Upload(context.Background(), body)
		Expect(err).To(MatchError(ContainSubstring("checking pending image upgrade")))
		Expect(body.Len()).To(Equal(len(archive)))
		snapshot, err := s.Status(context.Background())
		Expect(err).To(HaveOccurred())
		Expect(snapshot.Upload.State).To(Equal("idle"))
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
		for _, field := range []string{"upgrade", "upgradeError"} {
			Expect(result).To(HaveKeyWithValue(field, BeNil()))
		}
		Expect(result).To(HaveKeyWithValue("upload", HaveKeyWithValue("lastError", BeNil())))
		Expect(status().CurrentVersion).To(Equal("v0.31.0"))
	})

	It("removes failed preparation files and reports the error", func() {
		Expect(srvErrors.IsInvalidImageUpgradeImageError(upload([]byte("invalid OCI")))).To(BeTrue())
		Expect(status().Upload.State).To(Equal("idle"))
		Expect(status().Upload.LastError).NotTo(BeNil())
		files, err := os.ReadDir(filepath.Join(dir, "updates", ".staging"))
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(BeEmpty())
	})

	It("releases the upload slot and removes staged files after rejecting a sparse archive", func() {
		data, err := os.ReadFile(filepath.Join("testdata", "image_upgrade", "sparse-pax-1.0.tar"))
		Expect(err).NotTo(HaveOccurred())
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, err = s.Upload(ctx, bytes.NewReader(data))
		Expect(srvErrors.IsInvalidImageUpgradeImageError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("sparse archive entries are not supported"))
		staged, err := os.ReadDir(filepath.Join(dir, "updates", ".staging"))
		Expect(err).NotTo(HaveOccurred())
		Expect(staged).To(BeEmpty())
		published, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(published).To(BeEmpty())
		Expect(upload(archive)).To(Succeed())
	})

	It("does not publish a malformed layer even when its blob digest matches", func() {
		layer := []byte("test layer")
		rootFS := v1.RootFS{Type: "layers", DiffIDs: []ocidigest.Digest{ocidigest.FromBytes(layer)}}
		data, _ := test.ImageUpgradeArchiveWithLayers("v0.32.0", "v0.30.0", "", rootFS, []test.ImageUpgradeLayer{{Data: layer, MediaType: v1.MediaTypeImageLayer}})
		err := upload(data)
		Expect(srvErrors.IsInvalidImageUpgradeImageError(err)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("layer"))
		staged, err := os.ReadDir(filepath.Join(dir, "updates", ".staging"))
		Expect(err).NotTo(HaveOccurred())
		Expect(staged).To(BeEmpty())
		published, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		Expect(published).To(BeEmpty())
		Expect(upload(archive)).To(Succeed())
	})

	It("rejects connected mode", func() {
		connected = true
		Expect(srvErrors.IsModeConflictError(upload(archive))).To(BeTrue())
	})

	DescribeTable("enforces the three-times-space budget while streaming", func(extra int, fits bool) {
		services.SetImageUpgradeFreeSpaceForTest(s, func(string) (uint64, error) { return uint64(len(archive)*3 + extra), nil })
		err := upload(archive)
		if fits {
			Expect(err).NotTo(HaveOccurred())
		} else {
			Expect(srvErrors.IsImageUpgradeInsufficientStorageError(err)).To(BeTrue())
		}
	},
		Entry("insufficient space", -1, false),
		Entry("exactly enough space", 0, true),
		Entry("additional free space", 1, true),
	)

	It("maps out-of-space failures and removes partial files", func() {
		services.SetImageUpgradeFreeSpaceForTest(s, func(string) (uint64, error) { return 0, syscall.ENOSPC })
		Expect(srvErrors.IsImageUpgradeInsufficientStorageError(upload(archive))).To(BeTrue())
	})

	It("publishes only the complete upload by renaming and rejects concurrent uploads", func() {
		reader, writer := io.Pipe()
		defer func() { _ = reader.Close(); _ = writer.Close() }()
		done := make(chan error, 1)
		go func() { _, err := s.Upload(context.Background(), reader); done <- err }()
		Eventually(func() string { return status().Upload.State }).Should(Equal("uploading"))
		Expect(srvErrors.IsImageUpgradeInProgressError(upload(archive))).To(BeTrue())
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
		Expect(status().Upload.State).To(Equal("idle"))
	})

	It("cleans up a cancelled upload", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := s.Upload(ctx, bytes.NewReader(archive))
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(status().Upload.State).To(Equal("idle"))
	})

	It("removes abandoned partial uploads on startup without touching published images", func() {
		Expect(upload(archive)).To(Succeed())
		staging := filepath.Join(dir, "updates", ".staging")
		Expect(os.WriteFile(filepath.Join(staging, "orphan.part"), []byte("partial"), 0o600)).To(Succeed())
		published, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		s = newService()
		Expect(status().Upload.State).To(Equal("idle"))
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
		Expect(status().Upgrade.Result).To(Equal("applied"))
		Expect(status().CurrentVersion).To(Equal("v0.31.0"))
		Expect(os.WriteFile(path, []byte(`{"file":"v0.32.0.tar","result":"rolled-back","time":"2026-11-03T00:00:00Z","error":"failed","error_stage":"restart","running_image":{"version":"v0.31.0","digest":"sha256:old"},"last_successful_update":{"version":"v0.31.0","digest":"sha256:old","time":"2026-11-02T00:00:00Z"},"future":true}`), 0o600)).To(Succeed())
		Expect(status().Upgrade.Result).To(Equal("rolled-back"))
		Expect(status().Upgrade.RunningImage.Version).To(Equal("v0.31.0"))
		Expect(status().Upgrade.LastSuccessfulUpdate).NotTo(BeNil())
		Expect(os.WriteFile(path, []byte(`{"result":"failed"}`), 0o600)).To(Succeed())
		_, err := s.Status(context.Background())
		Expect(err).To(HaveOccurred())
		Expect(os.WriteFile(path, []byte(`bad JSON`), 0o600)).To(Succeed())
		_, err = s.Status(context.Background())
		Expect(err).To(HaveOccurred())
	})
})
