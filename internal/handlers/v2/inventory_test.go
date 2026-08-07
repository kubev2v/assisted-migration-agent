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

var _ = Describe("Inventory handler", func() {
	var (
		tmpDir  string
		agentID string
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		var err error
		tmpDir, err = os.MkdirTemp("", "handler-inventory-test-*")
		Expect(err).NotTo(HaveOccurred())
		agentID = uuid.New().String()
	})

	AfterEach(func() {
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	It("wraps the response in the v2 Inventory envelope", func() {
		ctx := context.Background()

		pool := store.NewPool(5 * time.Minute)

		mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(tmpDir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(mainDB.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
			return migrations.RunMain(ctx, db)
		})).To(Succeed())
		pool.Add(mainDB)

		collDB, err := pool.NewDatabase("coll-1", filepath.Join(tmpDir, "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
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
			return migrations.RunCollection(ctx, db, "collection")
		})).To(Succeed())
		pool.Add(collDB)

		collSt, err := collDB.Store()
		Expect(err).NotTo(HaveOccurred())

		inventoryJSON := `{"vcenter_id":"vc-test","clusters":{"cluster-1":{"vms":{"total":5},"infra":{}}}}`
		Expect(collSt.Inventory().Save(ctx, []byte(inventoryJSON))).To(Succeed())

		keyMgr, err := crypto.NewKeyManager("")
		Expect(err).NotTo(HaveOccurred())

		cfg := config.NewConfigurationWithOptionsAndDefaults(
			config.WithAgent(config.Agent{
				ID:       agentID,
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
		router.GET("/api/v2/inventory", handler.GetLatestInventory)

		req := httptest.NewRequest(http.MethodGet, "/api/v2/inventory", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		Expect(w.Code).To(Equal(http.StatusOK))

		var resp v2api.Inventory
		Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
		Expect(resp.Inventory.AgentId.String()).To(Equal(agentID))
		Expect(resp.Inventory.Inventory.VcenterId).To(Equal("vc-test"))
		Expect(resp.Inventory.Inventory.Clusters).To(HaveKey("cluster-1"))
	})
})
