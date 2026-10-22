package analyzer

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	v1 "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

type testServices struct {
	ServiceProvider
	files []string
}

func (s *testServices) StartRVToolsCollecting(files []string) (models.CollectorStatus, error) {
	s.files = files
	return models.CollectorStatus{State: models.CollectorStateConnecting}, nil
}

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
	if _, err := file.Write([]byte("fixture data")); err != nil {
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
	if err != nil || string(content) != "fixture data" {
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
