package services_test

import (
	"context"
	"database/sql"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("GroupService", func() {
	var (
		ctx context.Context
		db  *sql.DB
		st  *store.Store
		srv *services.GroupService
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db, test.NewMockValidator())
		Expect(st.Migrate(ctx)).To(Succeed())

		srv = services.NewGroupService(st)
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	Context("Create", func() {
		// Given a valid group definition
		// When we create the group through the service
		// Then it should persist the group and return it with a generated ID
		It("should create a group and return it with generated ID", func() {
			// Arrange
			group := models.Group{
				Name:        "production-vms",
				Filter:      "cluster = 'production'",
				Description: "All production VMs",
			}

			// Act
			created, err := srv.Create(ctx, group)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(created).NotTo(BeNil())
			Expect(created.ID).To(BeNumerically(">", 0))
			Expect(created.Name).To(Equal("production-vms"))
			Expect(created.Filter).To(Equal("cluster = 'production'"))
			Expect(created.Description).To(Equal("All production VMs"))
			Expect(created.CreatedAt).NotTo(BeZero())
			Expect(created.UpdatedAt).NotTo(BeZero())
		})

		// Given a group with the same name already exists
		// When we try to create another group with the same name
		// Then it should return a DuplicateResourceError
		It("should return duplicate error for existing name", func() {
			// Arrange
			group := models.Group{Name: "unique-group", Filter: "name = 'a'"}
			_, err := srv.Create(ctx, group)
			Expect(err).NotTo(HaveOccurred())

			// Act
			_, err = srv.Create(ctx, group)

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsDuplicateResourceError(err)).To(BeTrue())
		})

		// Given a group was created through the service
		// When we read it back from the database using raw SQL
		// Then the database row should match the created group
		It("should persist data readable by raw SQL", func() {
			// Arrange
			group := models.Group{
				Name:        "sql-check",
				Filter:      "memory >= 8GB",
				Description: "Verify raw SQL",
			}

			// Act
			created, err := srv.Create(ctx, group)
			Expect(err).NotTo(HaveOccurred())

			// Assert
			var name, filter, description string
			err = db.QueryRowContext(ctx,
				"SELECT name, filter, description FROM groups WHERE id = ?",
				created.ID).Scan(&name, &filter, &description)
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(Equal("sql-check"))
			Expect(filter).To(Equal("memory >= 8GB"))
			Expect(description).To(Equal("Verify raw SQL"))
		})
	})

	Context("Get", func() {
		// Given a group was inserted via raw SQL
		// When we retrieve it through the service
		// Then it should return the group with all fields
		It("should return a group inserted via raw SQL", func() {
			// Arrange
			_, err := db.ExecContext(ctx,
				`INSERT INTO groups (id, name, filter, description) VALUES (42, 'raw-group', 'name = ''test''', 'inserted via SQL')`)
			Expect(err).NotTo(HaveOccurred())

			// Act
			group, err := srv.Get(ctx, 42)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(group).NotTo(BeNil())
			Expect(group.ID).To(Equal(42))
			Expect(group.Name).To(Equal("raw-group"))
			Expect(group.Filter).To(Equal("name = 'test'"))
			Expect(group.Description).To(Equal("inserted via SQL"))
		})

		// Given no group exists with the requested ID
		// When we retrieve it through the service
		// Then it should return a ResourceNotFoundError
		It("should return not found for non-existent group", func() {
			// Act
			group, err := srv.Get(ctx, 999)

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
			Expect(group).To(BeNil())
		})
	})

	Context("List", func() {
		BeforeEach(func() {
			for _, g := range []models.Group{
				{Name: "alpha", Filter: "name = 'a'", Description: "first"},
				{Name: "beta", Filter: "name = 'b'", Description: "second"},
				{Name: "gamma", Filter: "name = 'c'", Description: "third"},
			} {
				_, err := srv.Create(ctx, g)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		// Given 3 groups exist in the database
		// When we list without filters
		// Then it should return all groups with the correct total
		It("should return all groups with total count", func() {
			// Act
			groups, total, err := srv.List(ctx, services.GroupListParams{})

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(3))
			Expect(groups).To(HaveLen(3))
		})

		// Given 3 groups exist in the database
		// When we list with limit 2
		// Then it should return 2 groups but total should still be 3
		It("should apply pagination", func() {
			// Arrange
			params := services.GroupListParams{Limit: 2, Offset: 0}

			// Act
			groups, total, err := srv.List(ctx, params)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(3))
			Expect(groups).To(HaveLen(2))
		})

		// Given 3 groups exist with names alpha, beta, gamma
		// When we list filtered by name "beta"
		// Then it should return only the beta group
		It("should filter by name", func() {
			// Arrange
			params := services.GroupListParams{ByName: "beta"}

			// Act
			groups, total, err := srv.List(ctx, params)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(1))
			Expect(groups).To(HaveLen(1))
			Expect(groups[0].Name).To(Equal("beta"))
		})

		// Given 3 groups exist in the database
		// When we list filtered by a name that doesn't exist
		// Then it should return an empty list with total 0
		It("should return empty for non-matching name filter", func() {
			// Arrange
			params := services.GroupListParams{ByName: "nonexistent"}

			// Act
			groups, total, err := srv.List(ctx, params)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(0))
			Expect(groups).To(BeEmpty())
		})
	})

	Context("Update", func() {
		// Given a group exists in the database
		// When we update its name through the service
		// Then the returned group should have the new name
		It("should update group fields", func() {
			// Arrange
			created, err := srv.Create(ctx, models.Group{
				Name: "original", Filter: "name = 'old'", Description: "old desc",
			})
			Expect(err).NotTo(HaveOccurred())

			updated := *created
			updated.Name = "renamed"
			updated.Description = "new desc"

			// Act
			result, err := srv.Update(ctx, created.ID, updated)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Name).To(Equal("renamed"))
			Expect(result.Description).To(Equal("new desc"))
			Expect(result.Filter).To(Equal("name = 'old'"))
			Expect(result.UpdatedAt.After(created.UpdatedAt) || result.UpdatedAt.Equal(created.UpdatedAt)).To(BeTrue())
		})

		// Given a group exists and we update it through the service
		// When we read the row back using raw SQL
		// Then the database should reflect the update
		It("should persist update readable by raw SQL", func() {
			// Arrange
			created, err := srv.Create(ctx, models.Group{
				Name: "before", Filter: "name = 'x'",
			})
			Expect(err).NotTo(HaveOccurred())

			updated := *created
			updated.Name = "after"

			// Act
			_, err = srv.Update(ctx, created.ID, updated)
			Expect(err).NotTo(HaveOccurred())

			// Assert
			var name string
			err = db.QueryRowContext(ctx, "SELECT name FROM groups WHERE id = ?", created.ID).Scan(&name)
			Expect(err).NotTo(HaveOccurred())
			Expect(name).To(Equal("after"))
		})

		// Given two groups exist with different names
		// When we update one to have the same name as the other
		// Then it should return a DuplicateResourceError
		It("should return duplicate error on name conflict", func() {
			// Arrange
			_, err := srv.Create(ctx, models.Group{Name: "taken", Filter: "name = 'a'"})
			Expect(err).NotTo(HaveOccurred())
			second, err := srv.Create(ctx, models.Group{Name: "free", Filter: "name = 'b'"})
			Expect(err).NotTo(HaveOccurred())

			conflict := *second
			conflict.Name = "taken"

			// Act
			_, err = srv.Update(ctx, second.ID, conflict)

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsDuplicateResourceError(err)).To(BeTrue())
		})

		// Given no group exists with the requested ID
		// When we try to update it
		// Then it should return a ResourceNotFoundError
		It("should return not found for non-existent group", func() {
			// Act
			_, err := srv.Update(ctx, 999, models.Group{Name: "x", Filter: "name = 'x'"})

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})
	})

	Context("Delete", func() {
		// Given a group exists in the database
		// When we delete it through the service
		// Then it should be removed from the database
		It("should delete an existing group", func() {
			// Arrange
			created, err := srv.Create(ctx, models.Group{Name: "doomed", Filter: "name = 'x'"})
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = srv.Delete(ctx, created.ID)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			var count int
			err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM groups WHERE id = ?", created.ID).Scan(&count)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(0))
		})

		// Given no group exists with the requested ID
		// When we try to delete it
		// Then it should return a ResourceNotFoundError
		It("should return not found for non-existent group", func() {
			// Act
			err := srv.Delete(ctx, 999)

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})
	})

	Context("ListVirtualMachines", func() {
		BeforeEach(func() {
			Expect(test.InsertVMs(ctx, db)).To(Succeed())
		})

		// Given a group with filter "cluster = 'production'" and VMs in production
		// When we list the group's virtual machines
		// Then it should return only VMs matching the filter
		It("should return VMs matching the group filter", func() {
			// Arrange
			created, err := srv.Create(ctx, models.Group{
				Name: "prod-vms", Filter: "cluster = 'production'",
			})
			Expect(err).NotTo(HaveOccurred())

			// Act
			vms, total, err := srv.ListVirtualMachines(ctx, created.ID, services.GroupGetParams{})

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(4))
			Expect(vms).To(HaveLen(4))
			for _, vm := range vms {
				Expect(vm.Cluster).To(Equal("production"))
			}
		})

		// Given a group with filter and pagination params
		// When we list the group's virtual machines with limit 2
		// Then it should return 2 VMs but total should reflect all matches
		It("should apply pagination to group VMs", func() {
			// Arrange
			created, err := srv.Create(ctx, models.Group{
				Name: "paged", Filter: "cluster = 'production'",
			})
			Expect(err).NotTo(HaveOccurred())

			params := services.GroupGetParams{Limit: 2, Offset: 0}

			// Act
			vms, total, err := srv.ListVirtualMachines(ctx, created.ID, params)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(4))
			Expect(vms).To(HaveLen(2))
		})

		// Given a group with a filter and sort params
		// When we list the group's virtual machines sorted by name ascending
		// Then the results should be in alphabetical order
		It("should sort group VMs", func() {
			// Arrange
			created, err := srv.Create(ctx, models.Group{
				Name: "sorted", Filter: "cluster = 'production'",
			})
			Expect(err).NotTo(HaveOccurred())

			params := services.GroupGetParams{
				Sort: []services.SortField{{Field: "name", Desc: false}},
			}

			// Act
			vms, _, err := srv.ListVirtualMachines(ctx, created.ID, params)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(len(vms)).To(BeNumerically(">", 1))
			for i := 1; i < len(vms); i++ {
				Expect(vms[i].Name >= vms[i-1].Name).To(BeTrue(),
					"expected %s >= %s", vms[i].Name, vms[i-1].Name)
			}
		})

		// Given a group with a filter that matches no VMs
		// When we list the group's virtual machines
		// Then it should return an empty list with total 0
		It("should return empty list when filter matches no VMs", func() {
			// Arrange
			created, err := srv.Create(ctx, models.Group{
				Name: "empty", Filter: "cluster = 'nonexistent'",
			})
			Expect(err).NotTo(HaveOccurred())

			// Act
			vms, total, err := srv.ListVirtualMachines(ctx, created.ID, services.GroupGetParams{})

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(0))
			Expect(vms).To(BeEmpty())
		})

		// Given no group exists with the requested ID
		// When we try to list its virtual machines
		// Then it should return a ResourceNotFoundError
		It("should return not found for non-existent group", func() {
			// Act
			vms, total, err := srv.ListVirtualMachines(ctx, 999, services.GroupGetParams{})

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
			Expect(vms).To(BeEmpty())
			Expect(total).To(Equal(0))
		})
	})
})
