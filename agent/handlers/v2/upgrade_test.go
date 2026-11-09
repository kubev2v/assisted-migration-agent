package v2_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v2 "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	handlers "github.com/kubev2v/assisted-migration-agent/agent/handlers/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/upgrade"
)

var _ = Describe("Host lifecycle controls", func() {
	var router *gin.Engine
	var lifecycle *upgrade.Service
	BeforeEach(func() {
		folder := GinkgoT().TempDir()
		var err error
		lifecycle, err = upgrade.NewService(folder, services.NewServiceManager(folder, services.WithPool(store.NewPool(time.Minute))), time.Second)
		Expect(err).NotTo(HaveOccurred())
		router = gin.New()
		group := router.Group("/api/v2")
		handler := handlers.NewUpgradeHandler(handlers.NewHandler(config.Configuration{}, nil), lifecycle)
		group.Use(handler.Admission)
		v2.RegisterHandlers(group, handler)
	})
	AfterEach(func() { lifecycle.Close() })
	DescribeTable("requires a real loopback peer and local secret", func(remote, token string, expected int) {
		if token == "valid" {
			token = lifecycle.Token()
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v2/upgrade", nil)
		req.RemoteAddr = remote
		req.Header.Set("X-Upgrade-Token", token)
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		Expect(response.Code).To(Equal(expected))
		if expected == http.StatusOK {
			var status map[string]any
			Expect(json.Unmarshal(response.Body.Bytes(), &status)).To(Succeed())
			Expect(status).To(HaveKeyWithValue("services_closed", false))
		}
	}, Entry("host", "127.0.0.1:1234", "valid", 200), Entry("remote spoofing forwarded IP", "192.0.2.1:1234", "valid", 403), Entry("wrong secret", "127.0.0.1:1234", "invalid", 403))
	It("blocks ordinary API handlers while draining, retaining version and control access", func() {
		Expect(lifecycle.BeginRequest()).To(BeTrue())
		defer lifecycle.EndRequest()
		Expect(lifecycle.Transition(upgrade.Request{Action: "maintenance", AttemptID: "api-test", PreviousImage: "sha256:" + strings.Repeat("a", 64), CandidateImage: "sha256:" + strings.Repeat("b", 64)})).To(Succeed())
		req := httptest.NewRequest(http.MethodPost, "/api/v2/collector", nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		Expect(response.Code).To(Equal(503))
		req = httptest.NewRequest(http.MethodGet, "/api/v2/version", nil)
		response = httptest.NewRecorder()
		router.ServeHTTP(response, req)
		Expect(response.Code).To(Equal(200))
	})
})
