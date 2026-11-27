package services_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"

	"github.com/google/uuid"
	agentAPI "github.com/kubev2v/migration-planner/api/v1alpha1/agent"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/pkg/console"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("ImageUpgradeService connected downloads", func() {
	var (
		dir       string
		s         *services.ImageUpgradeService
		connected bool
		archive   []byte
		sourceID  uuid.UUID
		server    *httptest.Server

		status       agentAPI.ImageUpgradeStatus
		statusCode   int
		image        func(w http.ResponseWriter)
		imageTokens  []string
		statusTokens []string
	)
	ptr := func(v string) *string { return &v }
	boolPtr := func(v bool) *bool { return &v }
	available := func() agentAPI.ImageUpgradeStatus {
		return agentAPI.ImageUpgradeStatus{
			UpdateAvailable: boolPtr(true),
			LatestVersion:   ptr("v0.32.0"),
			ApplianceImage:  &agentAPI.ApplianceImage{Supported: boolPtr(true), ImageUrl: ptr(server.URL + "/image")},
		}
	}
	serveImage := func(data []byte) func(http.ResponseWriter) {
		return func(w http.ResponseWriter) { _, _ = w.Write(data) }
	}
	published := func() []string {
		tars, err := filepath.Glob(filepath.Join(dir, "updates", "*.tar"))
		Expect(err).NotTo(HaveOccurred())
		return tars
	}
	uploadStatus := func() models.ImageUploadStatus {
		result, err := s.Status(context.Background())
		Expect(err).NotTo(HaveOccurred())
		return result.Upload
	}
	waitIdle := func() models.ImageUploadStatus {
		Eventually(func() string { return uploadStatus().State }).Should(Equal("idle"))
		return uploadStatus()
	}

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		connected = true
		sourceID = uuid.New()
		archive, _ = test.ImageUpgradeArchive("v0.32.0", "v0.30.0", "")
		statusCode = http.StatusOK
		image = serveImage(archive)
		imageTokens, statusTokens = nil, nil
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v1/sources/" + sourceID.String() + "/image-upgrade-status":
				statusTokens = append(statusTokens, r.Header.Get("X-Agent-Token"))
				if statusCode != http.StatusOK {
					w.WriteHeader(statusCode)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(status)
			case "/image":
				imageTokens = append(imageTokens, r.Header.Get("X-Agent-Token"))
				image(w)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		DeferCleanup(server.Close)
		status = available()

		var err error
		s, err = services.NewImageUpgradeService(dir, "v0.31.0", func(context.Context) (models.AgentMode, error) {
			if connected {
				return models.AgentModeConnected, nil
			}
			return models.AgentModeDisconnected, nil
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(s.Stop)
		client, err := console.NewConsoleClient(server.URL, "agent-jwt")
		Expect(err).NotTo(HaveOccurred())
		s.WithConsole(client, sourceID)
	})

	It("reports what the console serves", func() {
		latest, err := s.Latest(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(latest).To(Equal(models.ImageUpgradeLatest{UpdateAvailable: true, LatestVersion: ptr("v0.32.0"), Supported: true}))
		Expect(statusTokens).To(Equal([]string{"agent-jwt"}))
	})

	It("reports a new OVA requirement", func() {
		status.ApplianceImage = &agentAPI.ApplianceImage{Supported: boolPtr(false), Reason: ptr("new-ova-required")}
		latest, err := s.Latest(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(latest.Supported).To(BeFalse())
		Expect(latest.Reason).To(Equal(ptr("new-ova-required")))
	})

	It("reports no update as supported so the UI does not ask for a new OVA", func() {
		status = agentAPI.ImageUpgradeStatus{UpdateAvailable: boolPtr(false), LatestVersion: ptr("v0.31.0")}
		latest, err := s.Latest(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(latest).To(Equal(models.ImageUpgradeLatest{LatestVersion: ptr("v0.31.0"), Supported: true}))
	})

	It("requires connected mode", func() {
		connected = false
		_, err := s.Latest(context.Background())
		Expect(srvErrors.IsModeConflictError(err)).To(BeTrue())
		Expect(srvErrors.IsModeConflictError(s.StartDownload(context.Background()))).To(BeTrue())
		Expect(statusTokens).To(BeEmpty())
	})

	It("reports console failures", func() {
		statusCode = http.StatusInternalServerError
		_, err := s.Latest(context.Background())
		Expect(srvErrors.IsImageUpgradeConsoleError(err)).To(BeTrue())
		Expect(srvErrors.IsImageUpgradeConsoleError(s.StartDownload(context.Background()))).To(BeTrue())
	})

	It("downloads, validates and publishes the image without sending the agent token", func() {
		Expect(s.StartDownload(context.Background())).To(Succeed())
		Expect(waitIdle().LastError).To(BeNil())
		tars := published()
		Expect(tars).To(HaveLen(1))
		data, err := os.ReadFile(tars[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(Equal(archive))
		Expect(imageTokens).To(Equal([]string{""}))
	})

	DescribeTable("refuses to download when there is nothing to apply in place",
		func(update func(*agentAPI.ImageUpgradeStatus), check func(error) bool) {
			update(&status)
			Expect(check(s.StartDownload(context.Background()))).To(BeTrue())
			Expect(uploadStatus().State).To(Equal("idle"))
			Expect(imageTokens).To(BeEmpty())
		},
		Entry("no update", func(st *agentAPI.ImageUpgradeStatus) {
			st.UpdateAvailable, st.ApplianceImage = boolPtr(false), nil
		}, srvErrors.IsNoImageUpgradeError),
		Entry("new OVA required", func(st *agentAPI.ImageUpgradeStatus) {
			st.ApplianceImage = &agentAPI.ApplianceImage{Supported: boolPtr(false), Reason: ptr("new-ova-required")}
		}, srvErrors.IsNoImageUpgradeError),
		Entry("image not staged on the console yet", func(st *agentAPI.ImageUpgradeStatus) {
			st.ApplianceImage = &agentAPI.ApplianceImage{Supported: boolPtr(true)}
		}, srvErrors.IsImageUpgradeConsoleError),
	)

	It("returns a console error when the image request fails", func() {
		image = func(w http.ResponseWriter) { w.WriteHeader(http.StatusForbidden) }
		err := s.StartDownload(context.Background())
		Expect(srvErrors.IsImageUpgradeConsoleError(err)).To(BeTrue())
		Expect(err.Error()).NotTo(ContainSubstring(server.URL))
		Expect(uploadStatus().State).To(Equal("idle"))
		Expect(srvErrors.IsImageUpgradeConsoleError(s.StartDownload(context.Background()))).To(BeTrue(), "the lock is released after a failed start")
	})

	It("rejects a second download while one is running", func() {
		release := make(chan struct{})
		image = func(w http.ResponseWriter) {
			w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
			_, _ = w.Write(archive[:1024])
			w.(http.Flusher).Flush()
			<-release
			_, _ = w.Write(archive[1024:])
		}
		Expect(s.StartDownload(context.Background())).To(Succeed())
		Expect(uploadStatus().State).To(Equal("downloading"))
		Expect(srvErrors.IsImageUpgradeInProgressError(s.StartDownload(context.Background()))).To(BeTrue())
		close(release)
		Expect(waitIdle().LastError).To(BeNil())
		Expect(published()).To(HaveLen(1))
	})

	It("rejects a download while a published archive is pending", func() {
		Expect(s.StartDownload(context.Background())).To(Succeed())
		waitIdle()
		Expect(srvErrors.IsImageUpgradeInProgressError(s.StartDownload(context.Background()))).To(BeTrue())
		Expect(imageTokens).To(HaveLen(1))
	})

	DescribeTable("keeps failed downloads out of the watched directory and allows a retry",
		func(bad func([]byte) func(http.ResponseWriter)) {
			image = bad(archive)
			Expect(s.StartDownload(context.Background())).To(Succeed())
			Expect(waitIdle().LastError).NotTo(BeNil())
			Expect(published()).To(BeEmpty())
			staging, err := os.ReadDir(filepath.Join(dir, "updates", ".staging"))
			Expect(err).NotTo(HaveOccurred())
			Expect(staging).To(BeEmpty())

			image = serveImage(archive)
			Expect(s.StartDownload(context.Background())).To(Succeed())
			Expect(waitIdle().LastError).To(BeNil())
			Expect(published()).To(HaveLen(1))
		},
		Entry("connection dropped mid-download", func(data []byte) func(http.ResponseWriter) {
			return func(w http.ResponseWriter) {
				w.Header().Set("Content-Length", strconv.Itoa(len(data)))
				_, _ = w.Write(data[:len(data)/2])
			}
		}),
		Entry("corrupted image", func(data []byte) func(http.ResponseWriter) {
			corrupt := append([]byte(nil), data...)
			corrupt[len(corrupt)/2] ^= 0xff
			return serveImage(corrupt)
		}),
		Entry("unsupported version", func([]byte) func(http.ResponseWriter) {
			old, _ := test.ImageUpgradeArchive("v0.32.0", "v0.32.0", "")
			return serveImage(old)
		}),
	)
})
