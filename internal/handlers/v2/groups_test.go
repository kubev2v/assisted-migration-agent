package v2_test

import (
	"bytes"
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
	svc "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
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

var _ = Describe("Group write handlers (context timeout)", func() {
	var (
		handler *handlers.Handler
		router  *gin.Engine
		tmpDir  string
		pool    *store.Pool
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)

		var err error
		tmpDir, err = os.MkdirTemp("", "handler-group-write-test-*")
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
		Expect(database.Migrate(context.Background(), func(ctx context.Context, db *sql.DB) error {
			return migrations.RunCollection(ctx, db, "test")
		})).To(Succeed())

		groupSvc := svc.NewGroupService(st, &mockInventoryBuilder{})
		handler = handlers.NewHandler(config.Configuration{}, &stubServiceProvider{groupSvc: groupSvc})

		router = gin.New()
		router.POST("/groups", func(c *gin.Context) {
			handler.CreateLatestGroup(c)
		})
		router.PATCH("/groups/:groupId", func(c *gin.Context) {
			handler.UpdateLatestGroup(c, c.Param("groupId"))
		})
		router.DELETE("/groups/:groupId", func(c *gin.Context) {
			handler.DeleteLatestGroup(c, c.Param("groupId"))
		})
	})

	AfterEach(func() {
		pool.Close()
		os.RemoveAll(tmpDir) //nolint:errcheck
	})

	Describe("POST /groups (CreateLatestGroup)", func() {
		It("creates a group successfully within the write timeout", func() {
			body := `{"name": "timeout-test-group", "filter": "name = 'vm1'"}`
			req := httptest.NewRequest(http.MethodPost, "/groups", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusCreated))
			var resp v2api.Group
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Name).To(Equal("timeout-test-group"))
		})

		It("fails when request context is already cancelled", func() {
			body := `{"name": "should-fail", "filter": "name = 'vm1'"}`
			req := httptest.NewRequest(http.MethodPost, "/groups", bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")

			ctx, cancel := context.WithCancel(req.Context())
			cancel()
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})
	})

	Describe("PATCH /groups/{groupId} (UpdateLatestGroup)", func() {
		It("updates a group successfully within the write timeout", func() {
			createBody := `{"name": "update-me", "filter": "name = 'vm1'"}`
			createReq := httptest.NewRequest(http.MethodPost, "/groups", bytes.NewBufferString(createBody))
			createReq.Header.Set("Content-Type", "application/json")
			createW := httptest.NewRecorder()
			router.ServeHTTP(createW, createReq)
			Expect(createW.Code).To(Equal(http.StatusCreated))

			var created v2api.Group
			Expect(json.Unmarshal(createW.Body.Bytes(), &created)).To(Succeed())

			updateBody := `{"name": "updated-name"}`
			updateReq := httptest.NewRequest(http.MethodPatch, "/groups/"+created.Id, bytes.NewBufferString(updateBody))
			updateReq.Header.Set("Content-Type", "application/json")
			updateW := httptest.NewRecorder()

			router.ServeHTTP(updateW, updateReq)

			Expect(updateW.Code).To(Equal(http.StatusOK))
			var updated v2api.Group
			Expect(json.Unmarshal(updateW.Body.Bytes(), &updated)).To(Succeed())
			Expect(updated.Name).To(Equal("updated-name"))
		})

		It("fails when request context is already cancelled", func() {
			createBody := `{"name": "cancel-update", "filter": "name = 'vm1'"}`
			createReq := httptest.NewRequest(http.MethodPost, "/groups", bytes.NewBufferString(createBody))
			createReq.Header.Set("Content-Type", "application/json")
			createW := httptest.NewRecorder()
			router.ServeHTTP(createW, createReq)
			Expect(createW.Code).To(Equal(http.StatusCreated))

			var created v2api.Group
			Expect(json.Unmarshal(createW.Body.Bytes(), &created)).To(Succeed())

			updateBody := `{"name": "should-fail"}`
			updateReq := httptest.NewRequest(http.MethodPatch, "/groups/"+created.Id, bytes.NewBufferString(updateBody))
			updateReq.Header.Set("Content-Type", "application/json")

			ctx, cancel := context.WithCancel(updateReq.Context())
			cancel()
			updateReq = updateReq.WithContext(ctx)

			updateW := httptest.NewRecorder()
			router.ServeHTTP(updateW, updateReq)

			Expect(updateW.Code).To(BeNumerically(">=", 400))
		})
	})

	Describe("DELETE /groups/{groupId} (DeleteLatestGroup)", func() {
		It("deletes a group successfully within the write timeout", func() {
			createBody := `{"name": "delete-me", "filter": "name = 'vm1'"}`
			createReq := httptest.NewRequest(http.MethodPost, "/groups", bytes.NewBufferString(createBody))
			createReq.Header.Set("Content-Type", "application/json")
			createW := httptest.NewRecorder()
			router.ServeHTTP(createW, createReq)
			Expect(createW.Code).To(Equal(http.StatusCreated))

			var created v2api.Group
			Expect(json.Unmarshal(createW.Body.Bytes(), &created)).To(Succeed())

			deleteReq := httptest.NewRequest(http.MethodDelete, "/groups/"+created.Id, nil)
			deleteW := httptest.NewRecorder()

			router.ServeHTTP(deleteW, deleteReq)

			Expect(deleteW.Code).To(Equal(http.StatusNoContent))
		})

		It("fails when request context is already cancelled", func() {
			createBody := `{"name": "cancel-delete", "filter": "name = 'vm1'"}`
			createReq := httptest.NewRequest(http.MethodPost, "/groups", bytes.NewBufferString(createBody))
			createReq.Header.Set("Content-Type", "application/json")
			createW := httptest.NewRecorder()
			router.ServeHTTP(createW, createReq)
			Expect(createW.Code).To(Equal(http.StatusCreated))

			var created v2api.Group
			Expect(json.Unmarshal(createW.Body.Bytes(), &created)).To(Succeed())

			deleteReq := httptest.NewRequest(http.MethodDelete, "/groups/"+created.Id, nil)
			ctx, cancel := context.WithCancel(deleteReq.Context())
			cancel()
			deleteReq = deleteReq.WithContext(ctx)

			deleteW := httptest.NewRecorder()
			router.ServeHTTP(deleteW, deleteReq)

			Expect(deleteW.Code).To(Equal(http.StatusInternalServerError))
		})
	})
})
