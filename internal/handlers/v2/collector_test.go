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
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	v2api "github.com/kubev2v/assisted-migration-agent/api/v2"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	handlers "github.com/kubev2v/assisted-migration-agent/internal/handlers/v2"
	svc "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
)

var _ = Describe("GetCollectorStatus handler", func() {
	var (
		tmpDir string
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		var err error
		tmpDir, err = os.MkdirTemp("", "handler-collector-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	It("returns collected when a persisted collection with inventory exists but no collector is running", func() {
		ctx := context.Background()

		pool := store.NewPool(5 * time.Minute)

		mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(tmpDir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(mainDB.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
			return migrations.RunMain(ctx, db)
		})).To(Succeed())
		pool.Add(mainDB)

		collDB, err := pool.NewDatabase("coll-1", filepath.Join(tmpDir, "collection_999.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(collDB.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
			s, err := collDB.Store()
			if err != nil {
				return err
			}
			parser := duckdb_parser.New(s.Querier(), nil)
			if err := parser.Init(); err != nil {
				return err
			}
			return migrations.RunCollection(ctx, db, "collection_999")
		})).To(Succeed())
		pool.Add(collDB)

		collSt, err := collDB.Store()
		Expect(err).NotTo(HaveOccurred())
		Expect(collSt.Inventory().Save(ctx, []byte(`{"vms":1}`))).To(Succeed())

		keyMgr, err := crypto.NewKeyManager("")
		Expect(err).NotTo(HaveOccurred())

		cfg := config.NewConfigurationWithOptionsAndDefaults(
			config.WithAgent(config.Agent{
				ID:       uuid.New().String(),
				SourceID: uuid.New().String(),
				Mode:     "disconnected",
			}),
		)

		mgr := svc.NewServiceManager(
			svc.WithConfig(cfg),
			svc.WithPool(pool),
			svc.WithKeyManager(keyMgr),
		)
		Expect(mgr.Initialize()).To(Succeed())
		defer mgr.Stop(ctx)

		handler := handlers.NewHandler(*cfg, mgr)

		router := gin.New()
		router.GET("/api/v2/collector", handler.GetCollectorStatus)

		req := httptest.NewRequest(http.MethodGet, "/api/v2/collector", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		Expect(w.Code).To(Equal(http.StatusOK))

		var status v2api.CollectorStatus
		Expect(json.Unmarshal(w.Body.Bytes(), &status)).To(Succeed())
		Expect(status.Status).To(Equal(v2api.CollectorStatusStatusCollected))
	})
})
