package v2_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v2api "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	handlers "github.com/kubev2v/assisted-migration-agent/agent/handlers/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	svc "github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

type applianceProvider struct {
	stubServiceProvider
	applianceSvc *svc.ApplianceService
	groupSvc     *svc.GroupService
	applianceErr error
	groupErr     error
}

func (p *applianceProvider) ApplianceService(_ string) (*svc.ApplianceService, error) {
	if p.applianceErr != nil {
		return nil, p.applianceErr
	}
	return p.applianceSvc, nil
}

func (p *applianceProvider) LatestGroupService() (*svc.GroupService, error) {
	if p.groupErr != nil {
		return nil, p.groupErr
	}
	return p.groupSvc, nil
}

var _ = Describe("Appliances handlers", Ordered, func() {
	var (
		ctx      context.Context
		handler  *handlers.Handler
		groupSvc *svc.GroupService
		router   *gin.Engine
		tmpDir   string
		pool     *store.Pool
		groupID  string
	)

	BeforeAll(func() {
		ctx = context.Background()
		gin.SetMode(gin.TestMode)

		var err error
		tmpDir, err = os.MkdirTemp("", "handler-appliance-test-*")
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

		var sqlDB *sql.DB
		Expect(database.Migrate(ctx, func(mCtx context.Context, db *sql.DB) error {
			sqlDB = db
			return migrations.RunCollection(mCtx, db, "test")
		})).To(Succeed())

		Expect(insertVMForHandler(ctx, sqlDB, "vm-1", "web-server", nil)).To(Succeed())
		Expect(insertVMForHandler(ctx, sqlDB, "vm-2", "db-server", nil)).To(Succeed())
		Expect(insertVMForHandler(ctx, sqlDB, "vm-3", "empty-server", nil)).To(Succeed())

		_, err = sqlDB.ExecContext(ctx, `
			INSERT INTO vm_vapp_products (vm_id, key, name, vendor, version)
			VALUES
				('vm-1', 1, 'DataSync', 'AWS', '2.0'),
				('vm-2', 1, 'DataSync', 'AWS', '2.0'),
				('vm-2', 2, 'BIG-IP', 'F5', '17.1')
		`)
		Expect(err).NotTo(HaveOccurred())

		applianceSvc, err := svc.NewApplianceService(st)
		Expect(err).NotTo(HaveOccurred())

		groupSvc = svc.NewGroupService(st, &mockInventoryBuilder{})
		created, err := groupSvc.Create(ctx, models.Group{
			Name:   "web-group",
			Filter: "name = 'web-server'",
		})
		Expect(err).NotTo(HaveOccurred())
		groupID = created.ID.String()

		handler = handlers.NewHandler(config.Configuration{}, &applianceProvider{
			applianceSvc: applianceSvc,
			groupSvc:     groupSvc,
		})

		router = gin.New()
		router.GET("/collections/:id/appliances", func(c *gin.Context) {
			handler.ListAppliances(c, c.Param("id"))
		})
		router.GET("/groups/:groupId/appliances", func(c *gin.Context) {
			handler.ListGroupAppliances(c, c.Param("groupId"))
		})
	})

	AfterAll(func() {
		pool.Close()
		os.RemoveAll(tmpDir) //nolint:errcheck
	})

	Context("ListAppliances", func() {
		It("should return appliances for the collection", func() {
			// Act
			req := httptest.NewRequest(http.MethodGet, "/collections/col-1/appliances", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))

			var resp v2api.ApplianceListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Appliances).To(HaveLen(2))

			Expect(resp.Appliances[0].Name).To(Equal("BIG-IP"))
			Expect(resp.Appliances[0].VmCount).To(Equal(1))

			Expect(resp.Appliances[1].Name).To(Equal("DataSync"))
			Expect(resp.Appliances[1].VmCount).To(Equal(2))
			Expect(resp.Appliances[1].Vms).To(ConsistOf(
				v2api.VirtualMachineIdentity{Id: "vm-1", Name: "web-server"},
				v2api.VirtualMachineIdentity{Id: "vm-2", Name: "db-server"},
			))
		})

		It("should return 404 when the collection does not exist", func() {
			// Arrange
			notFoundHandler := handlers.NewHandler(config.Configuration{}, &applianceProvider{
				applianceErr: srvErrors.NewCollectionNotFoundError(),
			})
			notFoundRouter := gin.New()
			notFoundRouter.GET("/collections/:id/appliances", func(c *gin.Context) {
				notFoundHandler.ListAppliances(c, c.Param("id"))
			})

			// Act
			req := httptest.NewRequest(http.MethodGet, "/collections/missing/appliances", nil)
			w := httptest.NewRecorder()
			notFoundRouter.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusNotFound))
		})

		It("should return 500 when listing appliances fails", func() {
			// Arrange
			failingHandler := handlers.NewHandler(config.Configuration{}, &applianceProvider{
				applianceErr: errors.New("boom"),
			})
			failingRouter := gin.New()
			failingRouter.GET("/collections/:id/appliances", func(c *gin.Context) {
				failingHandler.ListAppliances(c, c.Param("id"))
			})

			// Act
			req := httptest.NewRequest(http.MethodGet, "/collections/col-1/appliances", nil)
			w := httptest.NewRecorder()
			failingRouter.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})
	})

	Context("ListGroupAppliances", func() {
		It("should return 400 for invalid group ID", func() {
			// Act
			req := httptest.NewRequest(http.MethodGet, "/groups/not-a-uuid/appliances", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should return 404 for non-existent group", func() {
			// Act
			req := httptest.NewRequest(http.MethodGet, "/groups/"+uuid.NewString()+"/appliances", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusNotFound))
		})

		It("should return 404 when there are no collections", func() {
			// Arrange
			noCollectionHandler := handlers.NewHandler(config.Configuration{}, &applianceProvider{
				groupErr: srvErrors.NewCollectionNotFoundError(),
			})
			noCollectionRouter := gin.New()
			noCollectionRouter.GET("/groups/:groupId/appliances", func(c *gin.Context) {
				noCollectionHandler.ListGroupAppliances(c, c.Param("groupId"))
			})

			// Act
			req := httptest.NewRequest(http.MethodGet, "/groups/"+groupID+"/appliances", nil)
			w := httptest.NewRecorder()
			noCollectionRouter.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusNotFound))
		})

		It("should return appliances for the group's VMs", func() {
			// Act
			req := httptest.NewRequest(http.MethodGet, "/groups/"+groupID+"/appliances", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))

			var resp v2api.ApplianceListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Appliances).To(HaveLen(1))
			Expect(resp.Appliances[0].Name).To(Equal("DataSync"))
			Expect(resp.Appliances[0].VmCount).To(Equal(1))
			Expect(resp.Appliances[0].Vms).To(Equal([]v2api.VirtualMachineIdentity{
				{Id: "vm-1", Name: "web-server"},
			}))
		})

		It("should return empty appliances when the group has none", func() {
			// Arrange
			emptyGroup, err := groupSvc.Create(ctx, models.Group{
				Name:   "empty-group",
				Filter: "name = 'empty-server'",
			})
			Expect(err).NotTo(HaveOccurred())

			// Act
			req := httptest.NewRequest(http.MethodGet, "/groups/"+emptyGroup.ID.String()+"/appliances", nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			// Assert
			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(w.Body.String()).To(ContainSubstring(`"appliances":[]`))
		})
	})
})
