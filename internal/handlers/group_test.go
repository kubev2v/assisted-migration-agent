package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1 "github.com/kubev2v/assisted-migration-agent/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/handlers"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var _ = Describe("Group Handlers", func() {
	var (
		mockGroup *MockGroupService
		handler   *handlers.Handler
		router    *gin.Engine
	)

	BeforeEach(func() {
		gin.SetMode(gin.TestMode)
		mockGroup = &MockGroupService{}
		handler = handlers.NewHandler(config.Configuration{}).WithGroupService(mockGroup)
		router = gin.New()
		router.GET("/vms/groups", func(c *gin.Context) {
			handler.ListGroups(c, v1.ListGroupsParams{})
		})
		router.POST("/vms/groups", handler.CreateGroup)
		router.GET("/vms/groups/:id", func(c *gin.Context) {
			handler.GetGroup(c, c.Param("id"), v1.GetGroupParams{})
		})
		router.PATCH("/vms/groups/:id", func(c *gin.Context) {
			handler.UpdateGroup(c, c.Param("id"))
		})
		router.DELETE("/vms/groups/:id", func(c *gin.Context) {
			handler.DeleteGroup(c, c.Param("id"))
		})
	})

	Context("ListGroups", func() {
		It("should return empty list when no groups exist", func() {
			mockGroup.ListResult = []models.Group{}
			mockGroup.ListTotal = 0

			req := httptest.NewRequest(http.MethodGet, "/vms/groups", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var resp v1.GroupListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Groups).To(BeEmpty())
			Expect(resp.Total).To(Equal(0))
			Expect(resp.Page).To(Equal(1))
			Expect(resp.PageCount).To(Equal(1))
		})

		It("should return all groups", func() {
			now := time.Now()
			mockGroup.ListResult = []models.Group{
				{ID: 1, Name: "group1", Filter: "memory >= 8GB", CreatedAt: now, UpdatedAt: now},
				{ID: 2, Name: "group2", Filter: "cluster = 'prod'", CreatedAt: now, UpdatedAt: now},
			}
			mockGroup.ListTotal = 2

			req := httptest.NewRequest(http.MethodGet, "/vms/groups", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var resp v1.GroupListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Groups).To(HaveLen(2))
			Expect(resp.Groups[0].Name).To(Equal("group1"))
			Expect(resp.Groups[1].Name).To(Equal("group2"))
			Expect(resp.Total).To(Equal(2))
			Expect(resp.Page).To(Equal(1))
		})

		It("should return 500 on service error", func() {
			mockGroup.ListError = errors.New("db error")

			req := httptest.NewRequest(http.MethodGet, "/vms/groups", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})
	})

	Context("CreateGroup", func() {
		It("should create a group and return 201", func() {
			now := time.Now()
			mockGroup.CreateResult = &models.Group{
				ID: 1, Name: "mygroup", Filter: "name = 'test'",
				Description: "desc", CreatedAt: now, UpdatedAt: now,
			}

			body := `{"name":"mygroup","filter":"name = 'test'","description":"desc"}`
			req := httptest.NewRequest(http.MethodPost, "/vms/groups", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusCreated))
			var resp v1.Group
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Name).To(Equal("mygroup"))
		})

		It("should return 400 when name is missing", func() {
			body := `{"filter":"name = 'test'"}`
			req := httptest.NewRequest(http.MethodPost, "/vms/groups", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should return 400 when name exceeds 100 characters", func() {
			longName := strings.Repeat("a", 101)
			body := `{"name":"` + longName + `","filter":"name = 'test'"}`
			req := httptest.NewRequest(http.MethodPost, "/vms/groups", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should return 400 when filter is missing", func() {
			body := `{"name":"mygroup"}`
			req := httptest.NewRequest(http.MethodPost, "/vms/groups", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should return 400 for invalid request body", func() {
			req := httptest.NewRequest(http.MethodPost, "/vms/groups", strings.NewReader("not json"))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should return 500 on service error", func() {
			mockGroup.CreateError = errors.New("db error")

			body := `{"name":"mygroup","filter":"name = 'test'"}`
			req := httptest.NewRequest(http.MethodPost, "/vms/groups", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})

		It("should return 400 when group name already exists", func() {
			mockGroup.CreateError = srvErrors.NewDuplicateResourceError("group", "name", "mygroup")

			body := `{"name":"mygroup","filter":"name = 'test'"}`
			req := httptest.NewRequest(http.MethodPost, "/vms/groups", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var resp map[string]string
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("already exists"))
		})
	})

	Context("GetGroup", func() {
		It("should return group with VMs", func() {
			now := time.Now()
			mockGroup.GetResult = &models.Group{
				ID: 1, Name: "mygroup", Filter: "name = 'test'",
				CreatedAt: now, UpdatedAt: now,
			}
			mockGroup.ListVMsResult = []models.VirtualMachineSummary{}
			mockGroup.ListVMsTotal = 0

			req := httptest.NewRequest(http.MethodGet, "/vms/groups/1", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var resp v1.GroupResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Group.Name).To(Equal("mygroup"))
			Expect(resp.Vms).To(BeEmpty())
			Expect(resp.Page).To(Equal(1))
			Expect(resp.PageCount).To(Equal(1))
		})

		It("should return 400 for non-numeric id", func() {
			req := httptest.NewRequest(http.MethodGet, "/vms/groups/abc", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should return 404 when group not found", func() {
			mockGroup.GetError = srvErrors.NewResourceNotFoundError("group", "999")

			req := httptest.NewRequest(http.MethodGet, "/vms/groups/999", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusNotFound))
		})

		It("should return 500 on service error", func() {
			mockGroup.GetError = errors.New("db error")

			req := httptest.NewRequest(http.MethodGet, "/vms/groups/1", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})
	})

	Context("UpdateGroup", func() {
		BeforeEach(func() {
			now := time.Now()
			mockGroup.GetResult = &models.Group{
				ID: 1, Name: "original", Filter: "name = 'old'",
				Description: "original desc", CreatedAt: now, UpdatedAt: now,
			}
			mockGroup.UpdateResult = &models.Group{
				ID: 1, Name: "updated", Filter: "name = 'old'",
				Description: "original desc", CreatedAt: now, UpdatedAt: now,
			}
		})

		It("should update name only", func() {
			body := `{"name":"updated"}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockGroup.LastUpdateGroup.Name).To(Equal("updated"))
			Expect(mockGroup.LastUpdateGroup.Filter).To(Equal("name = 'old'"))
			Expect(mockGroup.LastUpdateGroup.Description).To(Equal("original desc"))
		})

		It("should update filter only", func() {
			mockGroup.UpdateResult.Filter = "name = 'new'"

			body := `{"filter":"name = 'new'"}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockGroup.LastUpdateGroup.Name).To(Equal("original"))
			Expect(mockGroup.LastUpdateGroup.Filter).To(Equal("name = 'new'"))
		})

		It("should update description only", func() {
			mockGroup.UpdateResult.Description = "new desc"

			body := `{"description":"new desc"}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockGroup.LastUpdateGroup.Description).To(Equal("new desc"))
		})

		It("should return 400 when no fields provided", func() {
			body := `{}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should return 400 when name is empty string", func() {
			body := `{"name":""}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should return 400 when name exceeds 100 characters", func() {
			longName := strings.Repeat("a", 101)
			body := `{"name":"` + longName + `"}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should return 400 when filter is empty string", func() {
			body := `{"filter":""}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should return 400 for non-numeric id", func() {
			body := `{"name":"updated"}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/abc", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should return 404 when group not found", func() {
			mockGroup.GetError = srvErrors.NewResourceNotFoundError("group", "999")

			body := `{"name":"updated"}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/999", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusNotFound))
		})

		It("should return 500 on service update error", func() {
			mockGroup.UpdateError = errors.New("db error")

			body := `{"name":"updated"}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})

		It("should return 400 when updating to existing name", func() {
			mockGroup.UpdateError = srvErrors.NewDuplicateResourceError("group", "name", "existing-name")

			body := `{"name":"existing-name"}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var resp map[string]string
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("already exists"))
		})
	})

	Context("DeleteGroup", func() {
		It("should return 204 on successful delete", func() {
			req := httptest.NewRequest(http.MethodDelete, "/vms/groups/1", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusNoContent))
			Expect(mockGroup.LastDeleteID).To(Equal(1))
		})

		It("should return 204 when group does not exist (idempotent)", func() {
			mockGroup.DeleteError = srvErrors.NewResourceNotFoundError("group", "999")

			req := httptest.NewRequest(http.MethodDelete, "/vms/groups/999", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusNoContent))
		})

		It("should return 400 for non-numeric id", func() {
			req := httptest.NewRequest(http.MethodDelete, "/vms/groups/abc", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
		})

		It("should return 500 on service error", func() {
			mockGroup.DeleteError = errors.New("db error")

			req := httptest.NewRequest(http.MethodDelete, "/vms/groups/1", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
		})
	})
})
