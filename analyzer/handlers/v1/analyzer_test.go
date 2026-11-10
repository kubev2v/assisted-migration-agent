package analyzer_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

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
	files    []string
	updateOp bool
	startErr error
	status   models.CollectorStatus
}

func (s *testServices) StartRVToolsCollecting(files []string, isUpdateOp bool) (models.CollectorStatus, error) {
	s.files = files
	s.updateOp = isUpdateOp
	return models.CollectorStatus{State: models.CollectorStateConnecting}, s.startErr
}

func (s *testServices) GetCollectorStatus() models.CollectorStatus {
	return s.status
}

func (s *testServices) StopCollecting() error {
	return s.startErr
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

		_, err = file.Write([]byte("PK\x03\x04fixture data"))
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
		Expect(string(content)).To(Equal("PK\x03\x04fixture data"))
	})

	DescribeTable("does not expose agent-only routes",
		func(method, path string) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(method, path, nil))

			Expect(response.Code).To(Equal(http.StatusNotFound))
		},
		Entry(
			"agent status",
			http.MethodGet,
			"/api/v1/agent",
		),

		Entry(
			"credentials",
			http.MethodGet,
			"/api/v1/credentials",
		),

		Entry(
			"inspector",
			http.MethodGet,
			"/api/v1/inspector",
		),

		Entry(
			"collector",
			http.MethodPost,
			"/api/v1/collector",
		),

		Entry(
			"vm inspection",
			http.MethodDelete,
			"/api/v1/virtualmachines/vm-1/inspection",
		),

		Entry(
			"v2 agent",
			http.MethodGet,
			"/api/v2/agent",
		),
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
	Entry(
		"rejects a malformed vCenter ID",
		"not-a-uuid",
		srvErrors.NewResourceNotFoundError("collection", ""),
		http.StatusBadRequest,
	),

	Entry(
		"returns not found when no collection exists",
		"11111111-1111-1111-1111-111111111111",
		srvErrors.NewResourceNotFoundError("collection", ""),
		http.StatusNotFound,
	),

	Entry(
		"returns internal error when the service fails",
		"11111111-1111-1111-1111-111111111111",
		errors.New("database unavailable"),
		http.StatusInternalServerError,
	),
)

type rvtoolsUpload struct {
	name    string
	content string
	size    int64
}

var _ = Describe("RVTools uploads", func() {
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

	DescribeTable("validates the complete batch before saving or collecting",
		func(uploads []rvtoolsUpload, want string) {
			request := newRVToolsUploadRequest(uploads...)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			files, err := os.ReadDir(dataDir)
			Expect(err).NotTo(HaveOccurred())

			if want == "" {
				Expect(response.Code).To(Equal(http.StatusAccepted))
				Expect(svc.files).To(HaveLen(len(uploads)))
				Expect(files).To(HaveLen(len(uploads)))
				return
			}

			Expect(response.Code).To(Equal(http.StatusBadRequest))
			var fields map[string]string
			Expect(json.Unmarshal(response.Body.Bytes(), &fields)).To(Succeed())
			Expect(fields["error"]).To(Equal(want))
			Expect(svc.files).To(BeEmpty())
			Expect(files).To(BeEmpty())
		},
		Entry(
			"rejects an empty file after a valid file",
			[]rvtoolsUpload{
				{name: "first.xlsx", content: "PK\x03\x04first"},
				{name: "empty.xlsx"},
			},
			"empty.xlsx: rvtools file is empty",
		),

		Entry(
			"rejects an oversized file",
			[]rvtoolsUpload{
				{name: "large.xlsx", content: "PK\x03\x04data", size: (50 << 20) + 1},
			},
			"large.xlsx: file exceeds maximum upload size of 50 MiB",
		),

		Entry(
			"rejects an invalid signature",
			[]rvtoolsUpload{
				{name: "invalid.xlsx", content: "not excel"},
			},
			"invalid.xlsx: file is not a valid Excel file",
		),

		Entry(
			"rejects a short signature",
			[]rvtoolsUpload{
				{name: "short.xlsx", content: "PK"},
			},
			"short.xlsx: file is not a valid Excel file",
		),

		Entry(
			"reports every invalid file in filename order",
			[]rvtoolsUpload{
				{name: "z-empty.xlsx"},
				{name: "valid.xlsx", content: "PK\x03\x04data"},
				{name: "a-invalid.xlsx", content: "{}"},
				{name: "m-large.xlsx", content: "PK\x03\x04data", size: (50 << 20) + 1},
			},
			"a-invalid.xlsx: file is not a valid Excel file\n"+
				"m-large.xlsx: file exceeds maximum upload size of 50 MiB\n"+
				"z-empty.xlsx: rvtools file is empty",
		),

		Entry(
			"combines errors for repeated filenames",
			[]rvtoolsUpload{
				{name: "same.xlsx"},
				{name: "same.xlsx", content: "{}"},
			},
			"same.xlsx: rvtools file is empty; file is not a valid Excel file",
		),

		Entry(
			"accepts the exact size limit",
			[]rvtoolsUpload{
				{name: "limit.xlsx", content: "PK\x03\x04data", size: 50 << 20},
			},
			"",
		),

		Entry(
			"accepts valid signatures",
			[]rvtoolsUpload{
				{name: "first.xlsx", content: "PK\x03\x04first"},
				{name: "second.xlsx", content: "PK\x03\x04second"},
			},
			"",
		),

		Entry(
			"requires at least one file",
			[]rvtoolsUpload{},
			"at least one file is required",
		),
	)

	It("validates update uploads before starting collection", func() {
		request := newRVToolsUploadRequest(rvtoolsUpload{name: "empty.xlsx"})
		request.Method = http.MethodPut
		response := httptest.NewRecorder()

		router.ServeHTTP(response, request)

		Expect(response.Code).To(Equal(http.StatusBadRequest))
		Expect(svc.files).To(BeEmpty())
		files, err := os.ReadDir(dataDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(BeEmpty())
	})

	It("starts valid uploads through the update collection path", func() {
		request := newRVToolsUploadRequest(rvtoolsUpload{name: "valid.xlsx", content: "PK\x03\x04data"})
		request.Method = http.MethodPut
		response := httptest.NewRecorder()

		router.ServeHTTP(response, request)

		Expect(response.Code).To(Equal(http.StatusAccepted))
		Expect(svc.files).To(HaveLen(1))
		Expect(svc.updateOp).To(BeTrue())
	})

	It("rejects malformed multipart data", func() {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/collector/rvtools", bytes.NewBufferString("broken"))
		request.Header.Set("Content-Type", "multipart/form-data; boundary=missing")
		response := httptest.NewRecorder()

		router.ServeHTTP(response, request)

		Expect(response.Code).To(Equal(http.StatusBadRequest))
		Expect(response.Body.String()).To(MatchJSON(`{"error":"invalid multipart form"}`))
		Expect(svc.files).To(BeEmpty())
	})

	DescribeTable("returns safe errors",
		func(method, path string, cause error, wantStatus int, wantMessage string) {
			svc.startErr = cause
			svc.status = models.CollectorStatus{State: models.CollectorStateError, Error: cause}
			request := newRVToolsUploadRequest(rvtoolsUpload{name: "private.xlsx", content: "PK\x03\x04fixture"})
			if method != http.MethodPost {
				request = httptest.NewRequest(method, path, nil)
			}
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			Expect(response.Code).To(Equal(wantStatus))
			var fields map[string]any
			Expect(json.Unmarshal(response.Body.Bytes(), &fields)).To(Succeed())
			Expect(fields["error"]).To(Equal(wantMessage))
			Expect(response.Body.String()).NotTo(ContainSubstring("/private/"))

			files, err := os.ReadDir(dataDir)
			Expect(err).NotTo(HaveOccurred())
			Expect(files).To(BeEmpty())
		},
		Entry(
			"starting collection",
			http.MethodPost,
			"/api/v1/collector/rvtools",
			errors.New("SELECT secret FROM /private/agent.duckdb"),
			http.StatusInternalServerError,
			duckdb_parser.RVToolsFailureMessage,
		),

		Entry(
			"polling collection",
			http.MethodGet,
			"/api/v1/collector",
			errors.New("SELECT secret FROM /private/agent.duckdb"),
			http.StatusOK,
			duckdb_parser.RVToolsFailureMessage,
		),

		Entry(
			"stopping collection",
			http.MethodDelete,
			"/api/v1/collector",
			errors.New("SELECT secret FROM /private/agent.duckdb"),
			http.StatusInternalServerError,
			duckdb_parser.RVToolsFailureMessage,
		),

		Entry(
			"collection already running",
			http.MethodPost,
			"/api/v1/collector/rvtools",
			fmt.Errorf("internal /private/context: %w", srvErrors.NewCollectionInProgressError()),
			http.StatusConflict,
			"collection already in progress",
		),
	)

	It("does not start collection when saving fails", func() {
		blockedDir := filepath.Join(dataDir, "not-a-directory")
		Expect(os.WriteFile(blockedDir, []byte("blocked"), 0600)).To(Succeed())
		handler := analyzer.NewHandler(config.Configuration{Agent: config.Agent{DataFolder: blockedDir}}, svc)
		router = gin.New()
		router.POST("/api/v1/collector/rvtools", handler.StartRvtoolsCollector)
		request := newRVToolsUploadRequest(rvtoolsUpload{name: "private.xlsx", content: "PK\x03\x04fixture"})
		response := httptest.NewRecorder()

		router.ServeHTTP(response, request)

		Expect(response.Code).To(Equal(http.StatusInternalServerError))
		Expect(response.Body.String()).To(MatchJSON(`{"error":"failed to save file"}`))
		Expect(svc.files).To(BeEmpty())
	})

	It("logs signature read failures and returns a safe server error", func() {
		core, logs := observer.New(zap.ErrorLevel)
		DeferCleanup(zap.ReplaceGlobals(zap.New(core)))
		spillDir := GinkgoT().TempDir()
		GinkgoT().Setenv("TMPDIR", spillDir)
		request := newRVToolsUploadRequest(rvtoolsUpload{name: "valid.xlsx", content: "PK\x03\x04workbook"})
		Expect(request.ParseMultipartForm(0)).To(Succeed())
		DeferCleanup(request.MultipartForm.RemoveAll)

		files, err := os.ReadDir(spillDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(HaveLen(1))

		// Replace our temporary upload with a directory to force a signature read failure.
		path := filepath.Join(spillDir, files[0].Name())
		Expect(os.Remove(path)).To(Succeed())
		Expect(os.Mkdir(path, 0700)).To(Succeed())
		response := httptest.NewRecorder()

		router.ServeHTTP(response, request)

		Expect(response.Code).To(Equal(http.StatusInternalServerError))
		Expect(response.Body.String()).To(MatchJSON(`{"error":"failed to read file"}`))
		entries := logs.FilterMessage("failed to read RVTools upload").All()
		Expect(entries).To(HaveLen(1))
		Expect(fmt.Sprint(entries[0].ContextMap()["error"])).To(ContainSubstring(path))
		Expect(svc.files).To(BeEmpty())
	})
})

func newRVToolsUploadRequest(uploads ...rvtoolsUpload) *http.Request {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, upload := range uploads {
		part, err := writer.CreateFormFile("files", upload.name)
		Expect(err).NotTo(HaveOccurred())

		_, err = part.Write([]byte(upload.content))
		Expect(err).NotTo(HaveOccurred())
	}
	Expect(writer.Close()).To(Succeed())

	request := httptest.NewRequest(http.MethodPost, "/api/v1/collector/rvtools", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	for i, upload := range uploads {
		if upload.size == 0 {
			continue
		}

		// Exercise size boundaries without allocating a 50 MiB fixture.
		Expect(request.ParseMultipartForm(32 << 20)).To(Succeed())
		request.MultipartForm.File["files"][i].Size = upload.size
	}

	return request
}
