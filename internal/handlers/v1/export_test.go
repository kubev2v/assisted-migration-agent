package v1_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	handlers "github.com/kubev2v/assisted-migration-agent/internal/handlers/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

var zipMagicBytes = []byte{0x50, 0x4b, 0x03, 0x04}

var _ = Describe("Export Handler", func() {
	var (
		mockExport *MockExportService
		handler    *handlers.Handler
		router     *gin.Engine
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		mockExport = &MockExportService{WriteZipResult: zipMagicBytes}
		handler = handlers.NewHandler(config.Configuration{}).WithExportService(mockExport)
		router = newExportRouter(handler)
	})

	type scopeCase struct {
		query      string
		wantScopes []string
		anyOrder   bool
	}

	DescribeTable("accepts valid scopes",
		func(c scopeCase) {
			w := serveExport(router, c.query)

			Expect(w.Code).To(Equal(http.StatusOK))
			if c.anyOrder {
				Expect(mockExport.WriteZipScopes).To(ConsistOf(c.wantScopes))
				return
			}
			Expect(mockExport.WriteZipScopes).To(Equal(c.wantScopes))
		},
		Entry("default when scope omitted", scopeCase{
			wantScopes: []string{"overview"},
		}),
		Entry("overview", scopeCase{query: "?scope=overview", wantScopes: []string{"overview"}}),
		Entry("hosts", scopeCase{query: "?scope=hosts", wantScopes: []string{"hosts"}}),
		Entry("clusters", scopeCase{query: "?scope=clusters", wantScopes: []string{"clusters"}}),
		Entry("datastores", scopeCase{query: "?scope=datastores", wantScopes: []string{"datastores"}}),
		Entry("vms", scopeCase{query: "?scope=vms", wantScopes: []string{"vms"}}),
		Entry("network", scopeCase{query: "?scope=network", wantScopes: []string{"network"}}),
		Entry("utilization", scopeCase{query: "?scope=utilization", wantScopes: []string{"utilization"}}),
		Entry("applications", scopeCase{query: "?scope=applications", wantScopes: []string{"applications"}}),
		Entry("groups", scopeCase{query: "?scope=groups", wantScopes: []string{"groups"}}),
		Entry("inspection", scopeCase{query: "?scope=inspection", wantScopes: []string{"inspection"}}),
		Entry("storage-forecast", scopeCase{query: "?scope=storage-forecast", wantScopes: []string{"storage-forecast"}}),
		Entry("all scopes", scopeCase{
			query: "?scope=overview,hosts,clusters,datastores,vms,network,utilization,applications,groups,inspection,storage-forecast",
			wantScopes: []string{
				"overview", "hosts", "clusters", "datastores", "vms", "network",
				"utilization", "applications", "groups", "inspection", "storage-forecast",
			},
			anyOrder: true,
		}),
		Entry("trim whitespace", scopeCase{
			query:      "?scope=overview%20,%20hosts",
			wantScopes: []string{"overview", "hosts"},
			anyOrder:   true,
		}),
		Entry("filter empty strings", scopeCase{
			query:      "?scope=overview,,hosts,",
			wantScopes: []string{"overview", "hosts"},
			anyOrder:   true,
		}),
		Entry("default when scope list is empty", scopeCase{
			query:      "?scope=,,",
			wantScopes: []string{"overview"},
		}),
		Entry("deduplicate scopes", scopeCase{
			query:      "?scope=overview,hosts,overview",
			wantScopes: []string{"overview", "hosts"},
		}),
	)

	It("returns ZIP download headers on success", func() {
		w := serveExport(router, "")

		Expect(w.Code).To(Equal(http.StatusOK))
		Expect(w.Header().Get("Content-Type")).To(Equal("application/zip"))
		Expect(w.Header().Get("Content-Disposition")).To(ContainSubstring("attachment"))
		Expect(mockExport.WriteZipCalled).To(BeTrue())
	})

	DescribeTable("returns 400 for invalid scopes",
		func(query string) {
			w := serveExport(router, query)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			Expect(w.Body.String()).To(ContainSubstring("invalid scope"))
		},
		Entry("unknown scope", "?scope=invalid"),
		Entry("partially invalid scopes", "?scope=overview,invalid"),
	)

	It("returns 500 when export generation fails", func() {
		mockExport.WriteZipError = errors.New("database error")

		w := serveExport(router, "?scope=overview")

		Expect(w.Code).To(Equal(http.StatusInternalServerError))
		Expect(w.Body.String()).To(ContainSubstring("export generation failed"))
		Expect(w.Body.String()).NotTo(ContainSubstring("database error"))
		Expect(w.Header().Get("Content-Disposition")).To(BeEmpty())
		Expect(w.Header().Get("Content-Type")).To(ContainSubstring("application/json"))
	})
})

func newExportRouter(handler *handlers.Handler) *gin.Engine {
	router := gin.New()
	router.GET("/export", func(c *gin.Context) {
		var params v1.ExportInventoryParams
		if err := c.ShouldBindQuery(&params); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		handler.ExportInventory(c, params)
	})
	return router
}

func serveExport(router *gin.Engine, query string) *httptest.ResponseRecorder {
	if query != "" && !strings.HasPrefix(query, "?") {
		query = "?" + query
	}
	req := httptest.NewRequest(http.MethodGet, "/export"+query, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// MockExportService implements handlers.ExportService for testing.
type MockExportService struct {
	WriteZipResult []byte
	WriteZipError  error
	WriteZipCalled bool
	WriteZipScopes []string
}

func (m *MockExportService) IsValidScope(scope string) bool {
	return store.IsExportScope(scope)
}

func (m *MockExportService) SupportedScopes() []string {
	return store.ExportSupportedScopes()
}

func (m *MockExportService) WriteZip(ctx context.Context, scopes []string, w io.Writer) error {
	m.WriteZipCalled = true
	m.WriteZipScopes = scopes
	if m.WriteZipError != nil {
		return m.WriteZipError
	}
	if len(m.WriteZipResult) > 0 {
		_, err := w.Write(m.WriteZipResult)
		return err
	}
	return nil
}
