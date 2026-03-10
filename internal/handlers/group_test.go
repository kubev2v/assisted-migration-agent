package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
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
			var params v1.ListGroupsParams
			if v := c.Query("page"); v != "" {
				p, _ := strconv.Atoi(v)
				params.Page = &p
			}
			if v := c.Query("pageSize"); v != "" {
				p, _ := strconv.Atoi(v)
				params.PageSize = &p
			}
			if v := c.Query("byName"); v != "" {
				params.ByName = &v
			}
			handler.ListGroups(c, params)
		})
		router.POST("/vms/groups", handler.CreateGroup)
		router.GET("/vms/groups/:id", func(c *gin.Context) {
			var params v1.GetGroupParams
			if v := c.Query("page"); v != "" {
				p, _ := strconv.Atoi(v)
				params.Page = &p
			}
			if v := c.Query("pageSize"); v != "" {
				p, _ := strconv.Atoi(v)
				params.PageSize = &p
			}
			if v, ok := c.GetQueryArray("sort"); ok {
				params.Sort = &v
			}
			handler.GetGroup(c, c.Param("id"), params)
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

		It("should pass page and pageSize params", func() {
			mockGroup.ListResult = []models.Group{}
			mockGroup.ListTotal = 0

			req := httptest.NewRequest(http.MethodGet, "/vms/groups?page=2&pageSize=5", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var resp v1.GroupListResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Page).To(Equal(2))
		})

		It("should pass byName param", func() {
			mockGroup.ListResult = []models.Group{}
			mockGroup.ListTotal = 0

			req := httptest.NewRequest(http.MethodGet, "/vms/groups?byName=production", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockGroup.LastListParams.ByName).To(Equal("production"))
		})

		It("should return 500 on service error", func() {
			mockGroup.ListError = errors.New("db error")

			req := httptest.NewRequest(http.MethodGet, "/vms/groups", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(Equal("db error"))
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
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("Name is required"))
		})

		It("should return 400 when name exceeds 100 characters", func() {
			longName := strings.Repeat("a", 101)
			body := `{"name":"` + longName + `","filter":"name = 'test'"}`
			req := httptest.NewRequest(http.MethodPost, "/vms/groups", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("Name must not exceed 100 characters"))
		})

		It("should return 400 when filter is missing", func() {
			body := `{"name":"mygroup"}`
			req := httptest.NewRequest(http.MethodPost, "/vms/groups", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("Filter is required"))
		})

		It("should return 400 for invalid request body", func() {
			req := httptest.NewRequest(http.MethodPost, "/vms/groups", strings.NewReader("not json"))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(Equal("invalid request body"))
		})

		It("should return 500 on service error", func() {
			mockGroup.CreateError = errors.New("db error")

			body := `{"name":"mygroup","filter":"name = 'test'"}`
			req := httptest.NewRequest(http.MethodPost, "/vms/groups", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(Equal("db error"))
		})

		It("should return 400 when name is only whitespace", func() {
			body := `{"name":"   ","filter":"name = 'test'"}`
			req := httptest.NewRequest(http.MethodPost, "/vms/groups", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var resp map[string]string
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("blank"))
		})

		It("should return 400 when filter is invalid", func() {
			body := `{"name":"mygroup","filter":"not a valid filter %%"}`
			req := httptest.NewRequest(http.MethodPost, "/vms/groups", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var resp map[string]string
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("filter"))
		})

		It("should pass tags to service", func() {
			now := time.Now()
			mockGroup.CreateResult = &models.Group{
				ID: 1, Name: "tagged", Filter: "name = 'test'",
				Tags: []string{"prod", "critical"}, CreatedAt: now, UpdatedAt: now,
			}

			body := `{"name":"tagged","filter":"name = 'test'","tags":["prod","critical"]}`
			req := httptest.NewRequest(http.MethodPost, "/vms/groups", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusCreated))
			Expect(mockGroup.LastCreateGroup.Tags).To(Equal([]string{"prod", "critical"}))
		})

		It("should return 400 when tag has invalid format", func() {
			body := `{"name":"mygroup","filter":"name = 'test'","tags":["valid_tag","bad tag!"]}`
			req := httptest.NewRequest(http.MethodPost, "/vms/groups", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("must contain only alphanumeric characters, underscores, and dots"))
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
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(Equal("invalid group id"))
		})

		It("should return 404 when group not found", func() {
			mockGroup.GetError = srvErrors.NewResourceNotFoundError("group", "999")

			req := httptest.NewRequest(http.MethodGet, "/vms/groups/999", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusNotFound))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("group '999' not found"))
		})

		It("should return 500 on service error", func() {
			mockGroup.GetError = errors.New("db error")

			req := httptest.NewRequest(http.MethodGet, "/vms/groups/1", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(Equal("db error"))
		})

		It("should pass page and pageSize params", func() {
			now := time.Now()
			mockGroup.GetResult = &models.Group{ID: 1, Name: "g", Filter: "name = 'x'", CreatedAt: now, UpdatedAt: now}
			mockGroup.ListVMsResult = []models.VirtualMachineSummary{}
			mockGroup.ListVMsTotal = 0

			req := httptest.NewRequest(http.MethodGet, "/vms/groups/1?page=3&pageSize=10", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var resp v1.GroupResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Page).To(Equal(3))
			Expect(mockGroup.LastListVMsParams.Limit).To(Equal(uint64(10)))
			Expect(mockGroup.LastListVMsParams.Offset).To(Equal(uint64(20)))
		})

		It("should pass valid sort params", func() {
			now := time.Now()
			mockGroup.GetResult = &models.Group{ID: 1, Name: "g", Filter: "name = 'x'", CreatedAt: now, UpdatedAt: now}
			mockGroup.ListVMsResult = []models.VirtualMachineSummary{}
			mockGroup.ListVMsTotal = 0

			req := httptest.NewRequest(http.MethodGet, "/vms/groups/1?sort=name:asc&sort=memory:desc", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockGroup.LastListVMsParams.Sort).To(HaveLen(2))
			Expect(mockGroup.LastListVMsParams.Sort[0].Field).To(Equal("name"))
			Expect(mockGroup.LastListVMsParams.Sort[0].Desc).To(BeFalse())
			Expect(mockGroup.LastListVMsParams.Sort[1].Field).To(Equal("memory"))
			Expect(mockGroup.LastListVMsParams.Sort[1].Desc).To(BeTrue())
		})

		It("should return 400 for invalid sort format", func() {
			now := time.Now()
			mockGroup.GetResult = &models.Group{ID: 1, Name: "g", Filter: "name = 'x'", CreatedAt: now, UpdatedAt: now}

			req := httptest.NewRequest(http.MethodGet, "/vms/groups/1?sort=invalid", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(Equal("invalid sort format, expected 'field:direction' (e.g., 'name:asc')"))
		})

		It("should return 400 for invalid sort field", func() {
			now := time.Now()
			mockGroup.GetResult = &models.Group{ID: 1, Name: "g", Filter: "name = 'x'", CreatedAt: now, UpdatedAt: now}

			req := httptest.NewRequest(http.MethodGet, "/vms/groups/1?sort=bogus:asc", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(Equal("invalid sort field: bogus"))
		})

		It("should return 400 for invalid sort direction", func() {
			now := time.Now()
			mockGroup.GetResult = &models.Group{ID: 1, Name: "g", Filter: "name = 'x'", CreatedAt: now, UpdatedAt: now}

			req := httptest.NewRequest(http.MethodGet, "/vms/groups/1?sort=name:up", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(Equal("invalid sort direction: up, must be 'asc' or 'desc'"))
		})

		It("should return 500 on ListVirtualMachines error", func() {
			now := time.Now()
			mockGroup.GetResult = &models.Group{ID: 1, Name: "g", Filter: "name = 'x'", CreatedAt: now, UpdatedAt: now}
			mockGroup.ListVMsError = errors.New("query failed")

			req := httptest.NewRequest(http.MethodGet, "/vms/groups/1", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(Equal("query failed"))
		})

		It("should return VMs in response", func() {
			now := time.Now()
			mockGroup.GetResult = &models.Group{ID: 1, Name: "g", Filter: "name = 'x'", CreatedAt: now, UpdatedAt: now}
			mockGroup.ListVMsResult = []models.VirtualMachineSummary{
				{ID: "vm-1", Name: "vm1"},
				{ID: "vm-2", Name: "vm2"},
			}
			mockGroup.ListVMsTotal = 2

			req := httptest.NewRequest(http.MethodGet, "/vms/groups/1", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			var resp v1.GroupResponse
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp.Vms).To(HaveLen(2))
			Expect(resp.Total).To(Equal(2))
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
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("at least one field must be provided"))
		})

		It("should return 400 when name is empty string", func() {
			body := `{"name":""}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("Name must be at least 1 characters"))
		})

		It("should return 400 when name is only whitespace", func() {
			body := `{"name":"   "}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var resp map[string]string
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("blank"))
		})

		It("should return 400 when filter is invalid syntax", func() {
			body := `{"filter":"not valid %%"}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var resp map[string]string
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("filter"))
		})

		It("should return 500 when Get returns non-404 error", func() {
			mockGroup.GetError = errors.New("db connection lost")

			body := `{"name":"updated"}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(Equal("db connection lost"))
		})

		It("should pass tags to service on update", func() {
			mockGroup.UpdateResult.Tags = []string{"staging"}

			body := `{"tags":["staging"]}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusOK))
			Expect(mockGroup.LastUpdateGroup.Tags).To(Equal([]string{"staging"}))
		})

		It("should return 400 when name exceeds 100 characters", func() {
			longName := strings.Repeat("a", 101)
			body := `{"name":"` + longName + `"}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("Name must not exceed 100 characters"))
		})

		It("should return 400 when filter is empty string", func() {
			body := `{"filter":""}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("Filter must be at least 1 characters"))
		})

		It("should return 400 for non-numeric id", func() {
			body := `{"name":"updated"}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/abc", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusBadRequest))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(Equal("invalid group id"))
		})

		It("should return 404 when group not found", func() {
			mockGroup.GetError = srvErrors.NewResourceNotFoundError("group", "999")

			body := `{"name":"updated"}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/999", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusNotFound))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(ContainSubstring("group '999' not found"))
		})

		It("should return 500 on service update error", func() {
			mockGroup.UpdateError = errors.New("db error")

			body := `{"name":"updated"}`
			req := httptest.NewRequest(http.MethodPatch, "/vms/groups/1", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(Equal("db error"))
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
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(Equal("invalid group id"))
		})

		It("should return 500 on service error", func() {
			mockGroup.DeleteError = errors.New("db error")

			req := httptest.NewRequest(http.MethodDelete, "/vms/groups/1", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			Expect(w.Code).To(Equal(http.StatusInternalServerError))
			var resp map[string]any
			Expect(json.Unmarshal(w.Body.Bytes(), &resp)).To(Succeed())
			Expect(resp["error"]).To(Equal("db error"))
		})
	})
})
