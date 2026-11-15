package v2_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v2api "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	handlers "github.com/kubev2v/assisted-migration-agent/agent/handlers/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("Agent VM details", func() {
	var router *gin.Engine

	BeforeEach(func() {
		ctx := context.Background()
		dir := GinkgoT().TempDir()
		pool := store.NewPool(time.Minute)
		DeferCleanup(pool.Close)
		db, err := pool.NewDatabase("collection", filepath.Join(dir, "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		pool.Add(db)
		st, err := db.Store()
		Expect(err).NotTo(HaveOccurred())
		Expect(duckdb_parser.New(st.Querier(), nil).Init()).To(Succeed())
		Expect(db.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
			return migrations.RunCollection(ctx, db, "collection")
		})).To(Succeed())
		_, err = st.Querier().ExecContext(ctx, test.VMWithDatastoreArrayIdentitySQL)
		Expect(err).NotTo(HaveOccurred())

		gin.SetMode(gin.TestMode)
		router = gin.New()
		mgr := services.NewServiceManager(dir, services.WithPool(pool))
		handler := handlers.NewHandler(config.Configuration{}, mgr)
		v2api.RegisterHandlersWithOptions(router, handler, v2api.GinServerOptions{BaseURL: "/api/v2"})
	})

	It("returns datastore array identity when available", func() {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/virtualmachines/vm-1", nil))
		Expect(response.Code).To(Equal(http.StatusOK), response.Body.String())
		test.ExpectVMDetailDatastoreArrayIdentity(response.Body.Bytes())
	})
})
