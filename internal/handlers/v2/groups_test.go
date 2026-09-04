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
	"github.com/kubev2v/migration-planner/pkg/inventory"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v2api "github.com/kubev2v/assisted-migration-agent/api/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	handlers "github.com/kubev2v/assisted-migration-agent/internal/handlers/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	svc "github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

var _ = Describe("GetGroup handler", func() {
	var (
		ctx     context.Context
		handler *handlers.Handler
		router  *gin.Engine
		tmpDir  string
		pool    *store.Pool
		groupID string
	)

	BeforeEach(func() {
		ctx = context.Background()
		gin.SetMode(gin.TestMode)

		var err error
		tmpDir, err = os.MkdirTemp("", "handler-group-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)
		database, err := pool.NewDatabase(
			"test",
			filepath.Join(tmpDir, "test.duckdb"),
			time.Now(),
			store.EagerConnectionInitilization,
			0,
			store.ReadWriteDatabase,
		)
		Expect(err).NotTo(HaveOccurred())

		st, err := database.Store()
		Expect(err).NotTo(HaveOccurred())

		Expect(duckdb_parser.New(st.Querier(), nil).Init()).To(Succeed())
		Expect(database.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
			return migrations.RunCollection(ctx, db, "test")
		})).To(Succeed())

		groupSvc := svc.NewGroupService(st, &mockInventoryBuilder{})

		// Create a group and store inventory for it.
		created, err := groupSvc.Create(ctx, models.Group{
			Name:   "test-group",
			Filter: "name = 'vm1'",
		})
		Expect(err).NotTo(HaveOccurred())
		groupID = created.ID.String()

		inv := &inventory.Inventory{VCenterID: "test-vcenter", VCenterVersion: "7.0.0"}
		Expect(st.Group().UpdateInventory(ctx, created.ID, inv)).To(Succeed())

		handler = handlers.NewHandler(config.Configuration{}, &stubServiceProvider{groupSvc: groupSvc})

		router = gin.New()
		router.GET("/collections/:id/groups/:groupId", func(c *gin.Context) {
			handler.GetGroup(c, c.Param("id"), c.Param("groupId"), v2api.GetGroupParams{})
		})
		router.GET("/groups/:groupId", func(c *gin.Context) {
			handler.GetLatestGroup(c, c.Param("groupId"), v2api.GetLatestGroupParams{})
		})
	})

	AfterEach(func() {
		pool.Close()
		os.RemoveAll(tmpDir) //nolint:errcheck
	})

	Context("GET /collections/{id}/groups/{groupId}", func() {
		It("includes inventory when the group has inventory data", func() {
			req := httptest.NewRequest(http.MethodGet, "/collections/coll-1/groups/"+groupID, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var resp v2api.GroupResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Inventory).NotTo(BeNil())
		})

		It("omits inventory when the group has none", func() {
			// Spin up a second isolated store with a group that has no inventory.
			tmpDir2, err := os.MkdirTemp("", "handler-group-noinv-*")
			Expect(err).NotTo(HaveOccurred())
			defer os.RemoveAll(tmpDir2) //nolint:errcheck

			pool2 := store.NewPool(5 * time.Minute)
			defer pool2.Close()
			database2, err := pool2.NewDatabase("test2", filepath.Join(tmpDir2, "test2.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			st2, err := database2.Store()
			Expect(err).NotTo(HaveOccurred())
			Expect(duckdb_parser.New(st2.Querier(), nil).Init()).To(Succeed())
			Expect(database2.Migrate(context.Background(), func(ctx context.Context, db *sql.DB) error {
				return migrations.RunCollection(ctx, db, "test2")
			})).To(Succeed())

			groupSvc3 := svc.NewGroupService(st2, &mockInventoryBuilder{})
			created2, err := groupSvc3.Create(context.Background(), models.Group{Name: "no-inv-group", Filter: "name = 'x'"})
			Expect(err).NotTo(HaveOccurred())

			handler2 := handlers.NewHandler(config.Configuration{}, &stubServiceProvider{groupSvc: groupSvc3})
			router2 := gin.New()
			router2.GET("/collections/:id/groups/:groupId", func(c *gin.Context) {
				handler2.GetGroup(c, c.Param("id"), c.Param("groupId"), v2api.GetGroupParams{})
			})

			req := httptest.NewRequest(http.MethodGet, "/collections/coll-1/groups/"+created2.ID.String(), nil)
			w := httptest.NewRecorder()
			router2.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var resp v2api.GroupResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Inventory).To(BeNil())
		})
	})

	Context("GET /groups/{groupId} (latest shortcut)", func() {
		It("includes inventory when the group has inventory data", func() {
			req := httptest.NewRequest(http.MethodGet, "/groups/"+groupID, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var resp v2api.GroupResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Inventory).NotTo(BeNil())
		})

		It("omits inventory when the group has none", func() {
			// Spin up a third isolated store with a group that has no inventory.
			tmpDir3, err := os.MkdirTemp("", "handler-group-noinv-latest-*")
			Expect(err).NotTo(HaveOccurred())
			defer os.RemoveAll(tmpDir3) //nolint:errcheck

			pool3 := store.NewPool(5 * time.Minute)
			defer pool3.Close()
			database3, err := pool3.NewDatabase("test3", filepath.Join(tmpDir3, "test3.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			st3, err := database3.Store()
			Expect(err).NotTo(HaveOccurred())
			Expect(duckdb_parser.New(st3.Querier(), nil).Init()).To(Succeed())
			Expect(database3.Migrate(context.Background(), func(ctx context.Context, db *sql.DB) error {
				return migrations.RunCollection(ctx, db, "test3")
			})).To(Succeed())

			groupSvc3 := svc.NewGroupService(st3, &mockInventoryBuilder{})
			created3, err := groupSvc3.Create(context.Background(), models.Group{Name: "no-inv-group-latest", Filter: "name = 'x'"})
			Expect(err).NotTo(HaveOccurred())

			handler3 := handlers.NewHandler(config.Configuration{}, &stubServiceProvider{groupSvc: groupSvc3})
			router3 := gin.New()
			router3.GET("/groups/:groupId", func(c *gin.Context) {
				handler3.GetLatestGroup(c, c.Param("groupId"), v2api.GetLatestGroupParams{})
			})

			req := httptest.NewRequest(http.MethodGet, "/groups/"+created3.ID.String(), nil)
			w := httptest.NewRecorder()
			router3.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var resp v2api.GroupResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Inventory).To(BeNil())
		})
	})
})
