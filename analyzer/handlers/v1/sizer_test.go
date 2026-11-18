package analyzer_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	oapimiddleware "github.com/oapi-codegen/gin-middleware"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	analyzer "github.com/kubev2v/assisted-migration-agent/analyzer/handlers/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

type sizerServices struct {
	analyzer.ServiceProvider
	err error
}

func (s *sizerServices) LatestSizerService() (*services.SizerService, error) {
	return nil, s.err
}

var _ = Describe("Cluster requirements handler", func() {
	var (
		router *gin.Engine
		pool   *store.Pool
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		pool = store.NewPool(time.Minute)
		router = gin.New()
		manager := services.NewServiceManager(GinkgoT().TempDir(), services.WithPool(pool))
		v1.RegisterHandlers(router.Group("/api/v1"), analyzer.NewHandler(config.Configuration{}, manager))
		DeferCleanup(pool.Close)
	})

	DescribeTable("rejects invalid input before looking up a collection",
		func(body string, message string) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/cluster-requirements", strings.NewReader(body))
			router.ServeHTTP(response, request)

			Expect(response.Code).To(Equal(http.StatusBadRequest))
			Expect(response.Body.String()).To(ContainSubstring(message))
		},
		Entry("empty body", "", "invalid request body"),
		Entry("malformed JSON", `{`, "invalid request body"),
		Entry("wrong field type", `{"workerNodeCPU":"large"}`, "invalid request body"),
		Entry("invalid VM filter", `{"filterExpression":"memory >"}`, "expression filter is invalid"),
	)

	It("returns 404 when there is no latest collection", func() {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/cluster-requirements", strings.NewReader(`{
			"filterExpression":"memory > 8GB", "cpuOverCommitRatio":"1:4",
			"memoryOverCommitRatio":"1:2", "workerNodeCPU":32, "workerNodeMemory":128
		}`))
		router.ServeHTTP(response, request)

		Expect(response.Code).To(Equal(http.StatusNotFound))
		Expect(response.Body.String()).To(MatchJSON(`{"error":"no collections found"}`))
	})

	DescribeTable("rejects omitted ratios through the API contract",
		func(body string) {
			spec, err := v1.GetSwagger()
			Expect(err).NotTo(HaveOccurred())
			router = gin.New()
			group := router.Group("/api/v1")
			group.Use(oapimiddleware.OapiRequestValidatorWithOptions(spec, &oapimiddleware.Options{SilenceServersWarning: true}))
			manager := services.NewServiceManager(GinkgoT().TempDir(), services.WithPool(pool))
			v1.RegisterHandlers(group, analyzer.NewHandler(config.Configuration{}, manager))

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/v1/cluster-requirements", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(response, request)

			Expect(response.Code).To(Equal(http.StatusBadRequest))
		},
		Entry("CPU ratio", `{"memoryOverCommitRatio":"1:2","workerNodeCPU":32,"workerNodeMemory":128}`),
		Entry("memory ratio", `{"cpuOverCommitRatio":"1:4","workerNodeCPU":32,"workerNodeMemory":128}`),
	)

	It("returns 500 when the latest sizing service cannot be created", func() {
		svc := &sizerServices{err: errors.New("collection storage is unavailable")}
		router = gin.New()
		v1.RegisterHandlers(router.Group("/api/v1"), analyzer.NewHandler(config.Configuration{}, svc))

		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/cluster-requirements", strings.NewReader(`{
			"cpuOverCommitRatio":"1:4", "memoryOverCommitRatio":"1:2",
			"workerNodeCPU":32, "workerNodeMemory":128
		}`))
		router.ServeHTTP(response, request)

		Expect(response.Code).To(Equal(http.StatusInternalServerError))
		Expect(response.Body.String()).To(MatchJSON(`{"error":"collection storage is unavailable"}`))
	})
})
