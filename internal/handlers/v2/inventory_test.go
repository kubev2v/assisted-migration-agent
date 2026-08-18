package v2_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
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
	"github.com/kubev2v/migration-planner/pkg/inventory"

	v1 "github.com/kubev2v/migration-planner/api/v1alpha1"
	agentAPI "github.com/kubev2v/migration-planner/api/v1alpha1/agent"

	v2api "github.com/kubev2v/assisted-migration-agent/api/v2"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	handlers "github.com/kubev2v/assisted-migration-agent/internal/handlers/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	svc "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
)

var _ = Describe("Inventory handler", func() {
	var (
		tmpDir  string
		agentID string
		pool    *store.Pool
		router  *gin.Engine
		stop    func()
	)

	// buildEnv creates a main + collection store, lets the caller seed data via
	// seed (called before the service manager starts), then wires up a router
	// that mirrors the generated scope query-parameter binding.
	buildEnv := func(ctx context.Context, seed func(collSt *store.Store2)) {
		pool = store.NewPool(5 * time.Minute)

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

		if seed != nil {
			seed(collSt)
		}

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
		stop = func() { mgr.Stop(ctx) }

		handler := handlers.NewHandler(*cfg, mgr)

		router = gin.New()
		router.GET("/api/v2/inventory", func(c *gin.Context) {
			var params v2api.GetLatestInventoryParams
			if v := c.Query("scope"); v != "" {
				scope := v2api.GetLatestInventoryParamsScope(v)
				params.Scope = &scope
			}
			handler.GetLatestInventory(c, params)
		})
	}

	saveMainInventory := func(collSt *store.Store2) {
		inventoryJSON := `{"vcenter_id":"vc-test","clusters":{"cluster-1":{"vms":{"total":5},"infra":{}}}}`
		Expect(collSt.Inventory().Save(context.Background(), []byte(inventoryJSON))).To(Succeed())
	}

	// doRequest issues GET /api/v2/inventory with an optional scope query param.
	doRequest := func(scope string) *httptest.ResponseRecorder {
		url := "/api/v2/inventory"
		if scope != "" {
			url += "?scope=" + scope
		}
		req := httptest.NewRequest(http.MethodGet, url, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	readZip := func(body []byte) map[string][]byte {
		zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		Expect(err).NotTo(HaveOccurred())
		files := make(map[string][]byte)
		for _, f := range zr.File {
			rc, err := f.Open()
			Expect(err).NotTo(HaveOccurred())
			data, err := io.ReadAll(rc)
			Expect(err).NotTo(HaveOccurred())
			Expect(rc.Close()).To(Succeed())
			files[f.Name] = data
		}
		return files
	}

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		var err error
		tmpDir, err = os.MkdirTemp("", "handler-inventory-test-*")
		Expect(err).NotTo(HaveOccurred())
		agentID = uuid.New().String()
	})

	AfterEach(func() {
		if stop != nil {
			stop()
			stop = nil
		}
		if pool != nil {
			pool.Close()
			pool = nil
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	Context("scope main (default)", func() {
		It("wraps the response in the v2 Inventory envelope when no scope is set", func() {
			ctx := context.Background()
			buildEnv(ctx, saveMainInventory)

			w := doRequest("")
			Expect(w.Code).To(Equal(http.StatusOK))

			var resp v2api.Inventory
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Inventory.AgentId.String()).To(Equal(agentID))
			Expect(resp.Inventory.Inventory.VcenterId).To(Equal("vc-test"))
			Expect(resp.Inventory.Inventory.Clusters).To(HaveKey("cluster-1"))
		})

		It("returns JSON when scope is explicitly main", func() {
			ctx := context.Background()
			buildEnv(ctx, saveMainInventory)

			w := doRequest("main")
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Header().Get("Content-Type")).To(ContainSubstring("application/json"))
		})
	})

	Context("scope all (bundle)", func() {
		It("returns a zip with only inventory.json when there are no groups", func() {
			ctx := context.Background()
			buildEnv(ctx, saveMainInventory)

			w := doRequest("all")
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Header().Get("Content-Type")).To(Equal("application/zip"))

			files := readZip(w.Body.Bytes())
			Expect(files).To(HaveKey("inventory.json"))
			Expect(files).To(HaveLen(1))

			var mainInv v1.Inventory
			Expect(json.Unmarshal(files["inventory.json"], &mainInv)).To(Succeed())
			Expect(mainInv.VcenterId).To(Equal("vc-test"))
		})

		It("returns a zip with inventory.json plus a subset file per group", func() {
			ctx := context.Background()
			var groupA, groupB string
			buildEnv(ctx, func(collSt *store.Store2) {
				saveMainInventory(collSt)

				createdA, err := collSt.Group().Create(ctx, models.Group{
					Name:   "group-a",
					Filter: "name = 'vm1'",
					Inventory: &inventory.Inventory{
						VCenterID: "vc-a",
						Clusters: map[string]inventory.InventoryData{
							"cluster-a": {VMs: inventory.VMsData{Total: 3}},
						},
					},
				})
				Expect(err).NotTo(HaveOccurred())
				groupA = createdA.ID.String()

				createdB, err := collSt.Group().Create(ctx, models.Group{
					Name:   "group-b",
					Filter: "name = 'vm2'",
					Inventory: &inventory.Inventory{
						VCenterID: "vc-b",
						Clusters: map[string]inventory.InventoryData{
							"cluster-b": {VMs: inventory.VMsData{Total: 7}},
						},
					},
				})
				Expect(err).NotTo(HaveOccurred())
				groupB = createdB.ID.String()
			})

			w := doRequest("all")
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Header().Get("Content-Type")).To(Equal("application/zip"))

			files := readZip(w.Body.Bytes())
			Expect(files).To(HaveKey("inventory.json"))
			Expect(files).To(HaveKey("subsets/" + groupA + ".json"))
			Expect(files).To(HaveKey("subsets/" + groupB + ".json"))
			Expect(files).To(HaveLen(3))

			var subsetA agentAPI.SourceSubsetUpdate
			Expect(json.Unmarshal(files["subsets/"+groupA+".json"], &subsetA)).To(Succeed())
			Expect(subsetA.Name).To(Equal("group-a"))
			Expect(subsetA.Inventory.VcenterId).To(Equal("vc-a"))
			Expect(subsetA.VcenterId).NotTo(BeNil())
			Expect(*subsetA.VcenterId).To(Equal("vc-a"))
			Expect(subsetA.VmsCount).NotTo(BeNil())
			Expect(*subsetA.VmsCount).To(Equal(3))
		})
	})
})
