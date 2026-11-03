package analyzer

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
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	v1 "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type testServices struct {
	ServiceProvider
	files    []string
	startErr error
	status   models.CollectorStatus
}

func (s *testServices) StartRVToolsCollecting(files []string) (models.CollectorStatus, error) {
	s.files = files
	return models.CollectorStatus{State: models.CollectorStateConnecting}, s.startErr
}

func (s *testServices) GetCollectorStatus() models.CollectorStatus { return s.status }

func (s *testServices) StopCollecting() error { return s.startErr }

func TestAnalyzerAPIExposesStatusAndRVToolsUpload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dataDir := t.TempDir()
	svc := &testServices{}
	router := gin.New()
	v1.RegisterHandlers(router.Group("/api/v1"), NewHandler(config.Configuration{Agent: config.Agent{DataFolder: dataDir}}, svc))

	status := httptest.NewRecorder()
	router.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/api/v1/status", nil))
	if status.Code != http.StatusOK || status.Body.String() != "{\"status\":\"ready\"}" {
		t.Fatalf("status response: %d %s", status.Code, status.Body.String())
	}

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("files", "sample.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("PK\x03\x04fixture data")); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	upload := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/collector/rvtools", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	router.ServeHTTP(upload, req)
	if upload.Code != http.StatusAccepted {
		t.Fatalf("upload response: %d %s", upload.Code, upload.Body.String())
	}
	if len(svc.files) != 1 || filepath.Dir(svc.files[0]) != dataDir {
		t.Fatalf("uploaded files: %v", svc.files)
	}
	content, err := os.ReadFile(svc.files[0])
	if err != nil || string(content) != "PK\x03\x04fixture data" {
		t.Fatalf("saved upload: %q, %v", content, err)
	}

	for _, endpoint := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/agent"},
		{http.MethodGet, "/api/v1/credentials"},
		{http.MethodGet, "/api/v1/inspector"},
		{http.MethodPost, "/api/v1/collector"},
		{http.MethodDelete, "/api/v1/virtualmachines/vm-1/inspection"},
		{http.MethodGet, "/api/v2/agent"},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(endpoint.method, endpoint.path, nil))
		if response.Code != http.StatusNotFound {
			t.Errorf("unexpected Analyzer route %s %s: %d", endpoint.method, endpoint.path, response.Code)
		}
	}
}

func TestAnalyzerRVToolsSafeErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cause := errors.New("SELECT secret FROM /var/lib/agent/private.duckdb")
	for _, problem := range []string{"start", "save", "poll", "stop", "conflict"} {
		t.Run(problem, func(t *testing.T) {
			svc := &testServices{startErr: cause, status: models.CollectorStatus{State: models.CollectorStateError, Error: cause}}
			dir := t.TempDir()
			if problem == "save" {
				dir = filepath.Join(dir, "not-a-directory")
				if err := os.WriteFile(dir, []byte("blocked"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			router := gin.New()
			h := NewHandler(config.Configuration{Agent: config.Agent{DataFolder: dir}}, svc)
			router.POST("/rvtools", h.StartRvtoolsCollector)
			router.GET("/collector", h.GetCollectorStatus)
			router.DELETE("/collector", h.StopCollector)
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("files", "private.xlsx")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write([]byte("PK\x03\x04fixture")); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/rvtools", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			expectedStatus := http.StatusInternalServerError
			expectedMessage := duckdb_parser.RVToolsFailureMessage
			if problem == "save" {
				expectedMessage = "failed to save file"
			}
			if problem == "stop" {
				req = httptest.NewRequest(http.MethodDelete, "/collector", nil)
			}
			if problem == "conflict" {
				svc.startErr = fmt.Errorf("internal /private/context: %w", srvErrors.NewCollectionInProgressError())
				expectedStatus = http.StatusConflict
				expectedMessage = "collection already in progress"
			}
			if problem == "poll" {
				req = httptest.NewRequest(http.MethodGet, "/collector", nil)
				expectedStatus = http.StatusOK
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			var fields map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
				t.Fatal(err)
			}
			if response.Code != expectedStatus || fields["error"] != expectedMessage {
				t.Fatalf("unsafe or unexpected response: %d %s", response.Code, response.Body.String())
			}
			if problem == "save" && len(svc.files) != 0 {
				t.Fatal("save failure must not start collection")
			}
		})
	}
}

func TestAnalyzerRVToolsUploadValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		content []byte
		size    int64
		want    string
	}{
		{name: "empty", content: []byte{}, want: "rvtools file is empty"},
		{name: "oversized", content: []byte("PK\x03\x04data"), size: maxRVToolsUploadSize + 1, want: "file exceeds maximum upload size of 50 MiB"},
		{name: "invalid signature", content: []byte("{}"), want: "file is not a valid Excel file"},
		{name: "short signature", content: []byte("PK"), want: "file is not a valid Excel file"},
		{name: "exact size limit", content: []byte("PK\x03\x04data"), size: maxRVToolsUploadSize},
		{name: "valid signature", content: []byte("PK\x03\x04data")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			svc := &testServices{}
			h := NewHandler(config.Configuration{Agent: config.Agent{DataFolder: dir}}, svc)
			router := gin.New()
			router.POST("/rvtools", h.StartRvtoolsCollector)
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			// A valid first file must not be saved if a later file fails validation.
			for i, content := range [][]byte{[]byte("PK\x03\x04first"), tc.content} {
				name := "first.xlsx"
				if i == 1 {
					name = "second.xlsx"
				}
				part, err := writer.CreateFormFile("files", name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := part.Write(content); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/rvtools", &body)
			request.Header.Set("Content-Type", writer.FormDataContentType())
			if tc.size != 0 {
				if err := request.ParseMultipartForm(32 << 20); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = request.MultipartForm.RemoveAll() }()
				// Exercise the size boundary without allocating a 50 MiB fixture.
				request.MultipartForm.File["files"][1].Size = tc.size
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if tc.want == "" {
				if response.Code != http.StatusAccepted || len(svc.files) != 2 {
					t.Fatalf("valid uploads rejected: %d %s", response.Code, response.Body.String())
				}
				return
			}
			var fields map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusBadRequest || fields["error"] != tc.want {
				t.Fatalf("unexpected validation response: %d %s", response.Code, response.Body.String())
			}
			files, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			if len(svc.files) != 0 || len(files) != 0 {
				t.Fatal("invalid batch must not save files or start collection")
			}
		})
	}
}

func TestAnalyzerRVToolsSignatureReadFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	core, logs := observer.New(zap.ErrorLevel)
	restore := zap.ReplaceGlobals(zap.New(core))
	defer restore()
	spillDir := t.TempDir()
	t.Setenv("TMPDIR", spillDir)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", "valid.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("PK\x03\x04workbook")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/rvtools", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := req.ParseMultipartForm(0); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = req.MultipartForm.RemoveAll() }()
	files, err := os.ReadDir(spillDir)
	if err != nil || len(files) != 1 {
		t.Fatalf("spill files: %v, %v", files, err)
	}
	// Inject a server-side read failure into our own temporary upload storage.
	path := filepath.Join(spillDir, files[0].Name())
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	services := &testServices{}
	handler := NewHandler(config.Configuration{Agent: config.Agent{DataFolder: t.TempDir()}}, services)
	router := gin.New()
	router.POST("/rvtools", handler.StartRvtoolsCollector)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusInternalServerError || response.Body.String() != `{"error":"failed to read file"}` {
		t.Fatalf("server-side signature read failure: want safe 500, got %d: %s", response.Code, response.Body.String())
	}
	entries := logs.FilterMessage("failed to read RVTools upload").All()
	if len(entries) != 1 || !strings.Contains(fmt.Sprint(entries[0].ContextMap()["error"]), path) {
		t.Fatalf("original read error missing from logs: %v", entries)
	}
	if len(services.files) != 0 {
		t.Fatal("signature read failure must not start collection")
	}
}
