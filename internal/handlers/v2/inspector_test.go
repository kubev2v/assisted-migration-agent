package v2_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v2api "github.com/kubev2v/assisted-migration-agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	handlers "github.com/kubev2v/assisted-migration-agent/internal/handlers/v2"
	svc "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type inspectorProvider struct {
	stubServiceProvider
	inspectorErr error
}

func (p *inspectorProvider) InspectorService() (*svc.InspectorService, error) {
	return nil, p.inspectorErr
}

var _ = Describe("Inspector handler – collection in progress", func() {
	var (
		router *gin.Engine
	)

	setup := func(err error) {
		gin.SetMode(gin.TestMode)
		provider := &inspectorProvider{inspectorErr: err}
		handler := handlers.NewHandler(config.Configuration{}, provider)
		router = gin.New()
		router.POST("/inspector", handler.StartInspection)
		router.GET("/inspector", func(c *gin.Context) {
			handler.GetInspectorStatus(c, v2api.GetInspectorStatusParams{})
		})
		router.DELETE("/inspector", handler.StopInspection)
		router.PUT("/inspector/vddk", handler.PutInspectorVddk)
	}

	DescribeTable("returns 409 when collection is in progress",
		func(method, path string) {
			setup(srvErrors.NewCollectionInProgressError())

			req := httptest.NewRequest(method, path, nil)
			if method == "POST" {
				req.Header.Set("Content-Type", "application/json")
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusConflict))
			var body map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &body)).To(Succeed())
			Expect(body["error"]).To(ContainSubstring("report is currently in progress"))
		},
		Entry("POST /inspector", http.MethodPost, "/inspector"),
		Entry("GET /inspector", http.MethodGet, "/inspector"),
		Entry("DELETE /inspector", http.MethodDelete, "/inspector"),
		Entry("PUT /inspector/vddk", http.MethodPut, "/inspector/vddk"),
	)

	DescribeTable("returns 400 when collection not found",
		func(method, path string) {
			setup(srvErrors.NewCollectionNotFoundError())

			req := httptest.NewRequest(method, path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		},
		Entry("POST /inspector", http.MethodPost, "/inspector"),
		Entry("GET /inspector", http.MethodGet, "/inspector"),
		Entry("DELETE /inspector", http.MethodDelete, "/inspector"),
		Entry("PUT /inspector/vddk", http.MethodPut, "/inspector/vddk"),
	)
})
