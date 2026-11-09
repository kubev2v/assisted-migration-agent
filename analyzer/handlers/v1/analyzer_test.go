package analyzer_test

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	analyzer "github.com/kubev2v/assisted-migration-agent/analyzer/handlers/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

func TestAnalyzerHandlers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Analyzer Handlers V1 Suite")
}

type testServices struct {
	analyzer.ServiceProvider
	files []string
}

func (s *testServices) StartRVToolsCollecting(files []string, isUpdateOp bool) (models.CollectorStatus, error) {
	s.files = files
	return models.CollectorStatus{State: models.CollectorStateConnecting}, nil
}

type inventoryDeleteServices struct {
	analyzer.ServiceProvider
	err error
}

func (s *inventoryDeleteServices) LatestInventoryService() (*services.InventoryService, error) {
	return nil, s.err
}

var _ = Describe("Analyzer API", func() {
	var (
		router  *gin.Engine
		svc     *testServices
		dataDir string
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		dataDir = GinkgoT().TempDir()
		svc = &testServices{}
		router = gin.New()
		v1.RegisterHandlers(router.Group("/api/v1"), analyzer.NewHandler(
			config.Configuration{Agent: config.Agent{DataFolder: dataDir}}, svc))
	})

	It("exposes the analyzer status", func() {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))

		Expect(response.Code).To(Equal(http.StatusOK))
		Expect(response.Body.String()).To(Equal(`{"status":"ready"}`))
	})

	It("saves the uploaded RVTools file and starts the collection", func() {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)

		file, err := form.CreateFormFile("files", "sample.xlsx")
		Expect(err).NotTo(HaveOccurred())

		_, err = file.Write([]byte("fixture data"))
		Expect(err).NotTo(HaveOccurred())
		Expect(form.Close()).To(Succeed())

		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/collector/rvtools", &body)
		request.Header.Set("Content-Type", form.FormDataContentType())
		router.ServeHTTP(response, request)

		Expect(response.Code).To(Equal(http.StatusAccepted))
		Expect(svc.files).To(HaveLen(1))
		Expect(filepath.Dir(svc.files[0])).To(Equal(dataDir))

		content, err := os.ReadFile(svc.files[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(Equal("fixture data"))
	})

	DescribeTable("does not expose agent-only routes",
		func(method, path string) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(method, path, nil))

			Expect(response.Code).To(Equal(http.StatusNotFound))
		},
		Entry("agent status", http.MethodGet, "/api/v1/agent"),
		Entry("credentials", http.MethodGet, "/api/v1/credentials"),
		Entry("inspector", http.MethodGet, "/api/v1/inspector"),
		Entry("collector", http.MethodPost, "/api/v1/collector"),
		Entry("vm inspection", http.MethodDelete, "/api/v1/virtualmachines/vm-1/inspection"),
		Entry("v2 agent", http.MethodGet, "/api/v2/agent"),
	)
})

var _ = DescribeTable("Deleting a vCenter inventory",
	func(id string, svcErr error, expectedStatus int) {
		gin.SetMode(gin.TestMode)
		router := gin.New()
		v1.RegisterHandlers(router.Group("/api/v1"), analyzer.NewHandler(
			config.Configuration{}, &inventoryDeleteServices{err: svcErr}))

		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodDelete, "/api/v1/inventory/vcenters/"+id, nil)
		router.ServeHTTP(response, request)

		Expect(response.Code).To(Equal(expectedStatus))
	},
	Entry("rejects a malformed vCenter ID",
		"not-a-uuid", srvErrors.NewResourceNotFoundError("collection", ""), http.StatusBadRequest),
	Entry("returns not found when no collection exists",
		"11111111-1111-1111-1111-111111111111", srvErrors.NewResourceNotFoundError("collection", ""), http.StatusNotFound),
	Entry("returns internal error when the service fails",
		"11111111-1111-1111-1111-111111111111", errors.New("database unavailable"), http.StatusInternalServerError),
)
