package v2_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/xuri/excelize/v2"

	v2api "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	handlers "github.com/kubev2v/assisted-migration-agent/agent/handlers/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

var _ = Describe("Collection storage export", func() {
	var router *gin.Engine

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		ctx := context.Background()
		dir := GinkgoT().TempDir()
		if extension := os.Getenv("DUCKDB_TEST_EXTENSION"); extension != "" {
			Expect(os.Symlink(extension, filepath.Join(dir, "sqlite_scanner.duckdb_extension"))).To(Succeed())
		}
		pool := store.NewPool(5 * time.Minute)
		DeferCleanup(pool.Close)
		db, err := pool.NewDatabase("coll-1", filepath.Join(dir, "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		pool.Add(db)
		st, err := db.Store()
		Expect(err).NotTo(HaveOccurred())
		Expect(duckdb_parser.New(st.Querier(), nil).Init()).To(Succeed())
		Expect(db.Migrate(ctx, func(ctx context.Context, sqlDB *sql.DB) error {
			return migrations.RunCollection(ctx, sqlDB, "collection")
		})).To(Succeed())
		mgr := services.NewServiceManager(dir, services.WithPool(pool))
		handler := handlers.NewHandler(config.Configuration{}, mgr)
		router = gin.New()
		v2api.RegisterHandlersWithOptions(router, handler, v2api.GinServerOptions{BaseURL: "/api/v2"})
	})

	DescribeTable("handles scope selection and formats", func(query string, status int, names []string, xlsx bool) {
		request := httptest.NewRequest(http.MethodGet, "/api/v2/collections/coll-1/export"+query, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		Expect(response.Code).To(Equal(status))
		if status != http.StatusOK {
			return
		}
		if xlsx {
			workbook, err := excelize.OpenReader(bytes.NewReader(response.Body.Bytes()))
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(workbook.Close)
			Expect(workbook.GetSheetList()).To(Equal(names))
		} else {
			archive, err := zip.NewReader(bytes.NewReader(response.Body.Bytes()), int64(response.Body.Len()))
			Expect(err).NotTo(HaveOccurred())
			actual := make([]string, 0, len(archive.File))
			for _, f := range archive.File {
				actual = append(actual, f.Name)
			}
			Expect(actual).To(ConsistOf(names))
		}
	},
		Entry("default", "", http.StatusOK, []string{"overview.csv"}, false),
		Entry("mixed and duplicate", "?scope=hbas,luns,hbas", http.StatusOK, []string{"hbas.csv", "luns.csv"}, false),
		Entry("invalid", "?scope=hbas,invalid", http.StatusBadRequest, nil, false),
		Entry("all storage ZIP", "?scope=overview,disks,datastores,hbas,luns,hosts", http.StatusOK, []string{"overview.csv", "disks.csv", "datastores.csv", "hbas.csv", "luns.csv", "hosts.csv"}, false),
		Entry("all storage workbook", "?scope=overview,disks,datastores,hbas,luns,hosts&format=xlsx", http.StatusOK, []string{"Overview", "Virtual Disks", "Datastores", "Host Storage Adapters", "Host Storage LUNs", "Hosts"}, true),
	)
})
