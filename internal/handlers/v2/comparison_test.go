package v2_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v2api "github.com/kubev2v/assisted-migration-agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	handlers "github.com/kubev2v/assisted-migration-agent/internal/handlers/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	svc "github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

// comparisonProvider overrides ComparisonService on the shared stubServiceProvider.
type comparisonProvider struct {
	stubServiceProvider
	svc *svc.ComparisonService
	err error
}

func (p *comparisonProvider) ComparisonService(_, _ string) (*svc.ComparisonService, error) {
	return p.svc, p.err
}

// buildHandlerTestStore creates a temporary DuckDB with the given VMs.
// Caller must os.RemoveAll(tmpDir) after the test.
func buildHandlerTestStore(vms []struct {
	id         string
	cluster    string
	migratable bool
}) (st *store.Store, db *store.Database, tmpDir string) {
	var err error
	tmpDir, err = os.MkdirTemp("", "handler-cmp-test-*")
	Expect(err).NotTo(HaveOccurred())

	pool := store.NewPool(5 * time.Minute)
	db, err = pool.NewDatabase(
		"test",
		filepath.Join(tmpDir, "test.duckdb"),
		time.Now(),
		store.EagerConnectionInitilization,
		0,
		store.ReadWriteDatabase,
	)
	Expect(err).NotTo(HaveOccurred())

	st, err = db.Store()
	Expect(err).NotTo(HaveOccurred())
	Expect(duckdb_parser.New(st.Querier(), nil).Init()).To(Succeed())

	ctx := context.Background()
	Expect(db.Migrate(ctx, func(ctx context.Context, rawDB *sql.DB) error {
		return migrations.RunCollection(ctx, rawDB, "test")
	})).To(Succeed())

	for _, vm := range vms {
		_, err := st.Querier().ExecContext(ctx,
			`INSERT INTO vinfo ("VM ID", "VM", "Cluster", "Powerstate", "Template", "Memory", "CPUs")
			 VALUES (?, ?, ?, 'poweredOn', false, 1024, 2)`,
			vm.id, vm.id, vm.cluster,
		)
		Expect(err).NotTo(HaveOccurred())
		if !vm.migratable {
			_, err = st.Querier().ExecContext(ctx,
				`INSERT INTO concerns ("VM_ID", "Concern_ID", "Label", "Category", "Assessment")
				 VALUES (?, ?, 'Critical issue', 'Critical', 'Must fix before migration')`,
				vm.id, "concern-"+vm.id,
			)
			Expect(err).NotTo(HaveOccurred())
		}
	}
	return st, db, tmpDir
}

var _ = Describe("CompareCollections handler", func() {
	var (
		handler *handlers.Handler
		router  *gin.Engine
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
	})

	setupRouter := func(provider handlers.ServiceProvider) {
		handler = handlers.NewHandler(config.Configuration{}, provider)
		router = gin.New()
		router.GET("/collections/compare/:aId/:bId", func(c *gin.Context) {
			handler.CompareCollections(c, c.Param("aId"), c.Param("bId"))
		})
	}

	It("returns 400 when aId and bId are the same", func() {
		setupRouter(&comparisonProvider{})
		req := httptest.NewRequest(http.MethodGet, "/collections/compare/same-id/same-id", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
	})

	It("returns 404 when a collection is not found", func() {
		setupRouter(&comparisonProvider{
			err: srvErrors.NewResourceNotFoundError("database", "missing-id"),
		})
		req := httptest.NewRequest(http.MethodGet, "/collections/compare/id-a/id-b", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		Expect(w.Code).To(Equal(http.StatusNotFound))
	})

	It("returns 200 with a valid summary for two populated collections", func() {
		aVMs := []struct {
			id         string
			cluster    string
			migratable bool
		}{
			{"vm-1", "cluster-a", true},
			{"vm-2", "cluster-a", false},
		}
		bVMs := []struct {
			id         string
			cluster    string
			migratable bool
		}{
			{"vm-1", "cluster-b", true},
			{"vm-3", "cluster-b", true},
		}

		stA, dbA, tmpA := buildHandlerTestStore(aVMs)
		stB, dbB, tmpB := buildHandlerTestStore(bVMs)
		DeferCleanup(func() { os.RemoveAll(tmpA); os.RemoveAll(tmpB) }) //nolint:errcheck

		realSvc := svc.NewComparisonService(stA, stB,
			models.CollectionMeta{ID: dbA.ID, CreatedAt: dbA.CreatedAt},
			models.CollectionMeta{ID: dbB.ID, CreatedAt: dbB.CreatedAt},
		)
		setupRouter(&comparisonProvider{svc: realSvc})

		req := httptest.NewRequest(http.MethodGet, "/collections/compare/id-a/id-b", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		Expect(w.Code).To(Equal(http.StatusOK))
		var resp v2api.CollectionComparisonSummary
		Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
		Expect(resp.Collections).To(HaveLen(2))
		Expect(resp.Diff.TotalVMs.Delta).To(Equal(0)) // 2 VMs each
	})
})

var _ = Describe("CompareCollectionsDiff handler", func() {
	var (
		handler *handlers.Handler
		router  *gin.Engine
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
	})

	setupRouter := func(provider handlers.ServiceProvider) {
		handler = handlers.NewHandler(config.Configuration{}, provider)
		router = gin.New()
		router.GET("/collections/compare/:aId/:bId/:dimension", func(c *gin.Context) {
			handler.CompareCollectionsDiff(
				c,
				c.Param("aId"),
				c.Param("bId"),
				v2api.CompareCollectionsDiffParamsDimension(c.Param("dimension")),
				v2api.CompareCollectionsDiffParams{},
			)
		})
	}

	It("returns 400 when aId and bId are the same", func() {
		setupRouter(&comparisonProvider{})
		req := httptest.NewRequest(http.MethodGet, "/collections/compare/same-id/same-id/migratable", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
	})

	It("returns 400 for an invalid dimension", func() {
		setupRouter(&comparisonProvider{})
		req := httptest.NewRequest(http.MethodGet, "/collections/compare/a/b/invalid-dim", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		Expect(w.Code).To(Equal(http.StatusBadRequest))
	})

	It("returns 404 when a collection is not found", func() {
		setupRouter(&comparisonProvider{
			err: srvErrors.NewResourceNotFoundError("database", "missing-id"),
		})
		req := httptest.NewRequest(http.MethodGet, "/collections/compare/id-a/id-b/migratable", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		Expect(w.Code).To(Equal(http.StatusNotFound))
	})

	It("returns 200 with paginated diff for a valid dimension", func() {
		aVMs := []struct {
			id         string
			cluster    string
			migratable bool
		}{
			{"vm-1", "c", true},
			{"vm-2", "c", false},
		}
		bVMs := []struct {
			id         string
			cluster    string
			migratable bool
		}{
			{"vm-1", "c", true},
			{"vm-3", "c", true},
		}

		stA, dbA, tmpA := buildHandlerTestStore(aVMs)
		stB, dbB, tmpB := buildHandlerTestStore(bVMs)
		DeferCleanup(func() { os.RemoveAll(tmpA); os.RemoveAll(tmpB) }) //nolint:errcheck

		realSvc := svc.NewComparisonService(stA, stB,
			models.CollectionMeta{ID: dbA.ID, CreatedAt: dbA.CreatedAt},
			models.CollectionMeta{ID: dbB.ID, CreatedAt: dbB.CreatedAt},
		)
		setupRouter(&comparisonProvider{svc: realSvc})

		req := httptest.NewRequest(http.MethodGet, "/collections/compare/id-a/id-b/migratable", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		Expect(w.Code).To(Equal(http.StatusOK))
		var resp v2api.CollectionComparisonDiff
		Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
		// vm-3 is migratable in B but absent from A
		Expect(resp.OnlyInB.VmIds).To(ConsistOf("vm-3"))
		Expect(resp.OnlyInA.VmIds).To(BeEmpty())
	})
})
