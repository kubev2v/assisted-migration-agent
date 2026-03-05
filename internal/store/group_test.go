package store_test

import (
	"context"
	"database/sql"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/filter"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("GroupStore", func() {
	var (
		ctx context.Context
		s   *store.Store
		db  *sql.DB
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		s = store.NewStore(db, test.NewMockValidator())
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	Context("List", func() {
		It("should return empty list when no groups exist", func() {
			groups, err := s.Group().List(ctx, nil, 0, 0)

			Expect(err).NotTo(HaveOccurred())
			Expect(groups).To(BeEmpty())
		})

		It("should return all groups", func() {
			g1 := models.Group{Name: "group1", Filter: "memory >= 8GB"}
			g2 := models.Group{Name: "group2", Filter: "cluster = 'prod'"}
			_, err := s.Group().Create(ctx, g1)
			Expect(err).NotTo(HaveOccurred())
			_, err = s.Group().Create(ctx, g2)
			Expect(err).NotTo(HaveOccurred())

			groups, err := s.Group().List(ctx, nil, 0, 0)

			Expect(err).NotTo(HaveOccurred())
			Expect(groups).To(HaveLen(2))
			Expect(groups[0].Name).To(Equal("group1"))
			Expect(groups[1].Name).To(Equal("group2"))
		})

		It("should filter by name", func() {
			_, err := s.Group().Create(ctx, models.Group{Name: "prod-cluster", Filter: "cluster = 'prod'"})
			Expect(err).NotTo(HaveOccurred())
			_, err = s.Group().Create(ctx, models.Group{Name: "staging-cluster", Filter: "cluster = 'staging'"})
			Expect(err).NotTo(HaveOccurred())

			f, err := filter.ParseWithGroupMap([]byte("name = 'prod-cluster'"))
			Expect(err).NotTo(HaveOccurred())

			groups, err := s.Group().List(ctx, []sq.Sqlizer{f}, 0, 0)

			Expect(err).NotTo(HaveOccurred())
			Expect(groups).To(HaveLen(1))
			Expect(groups[0].Name).To(Equal("prod-cluster"))
		})

		It("should paginate results", func() {
			for i := 0; i < 5; i++ {
				_, err := s.Group().Create(ctx, models.Group{
					Name: fmt.Sprintf("group-%d", i), Filter: "memory > 0",
				})
				Expect(err).NotTo(HaveOccurred())
			}

			groups, err := s.Group().List(ctx, nil, 2, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(groups).To(HaveLen(2))

			groups, err = s.Group().List(ctx, nil, 2, 2)
			Expect(err).NotTo(HaveOccurred())
			Expect(groups).To(HaveLen(2))

			groups, err = s.Group().List(ctx, nil, 2, 4)
			Expect(err).NotTo(HaveOccurred())
			Expect(groups).To(HaveLen(1))
		})
	})

	Context("Count", func() {
		It("should return 0 when no groups exist", func() {
			count, err := s.Group().Count(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(0))
		})

		It("should count all groups", func() {
			_, err := s.Group().Create(ctx, models.Group{Name: "g1", Filter: "memory > 0"})
			Expect(err).NotTo(HaveOccurred())
			_, err = s.Group().Create(ctx, models.Group{Name: "g2", Filter: "memory > 0"})
			Expect(err).NotTo(HaveOccurred())

			count, err := s.Group().Count(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(2))
		})

		It("should count with name filter", func() {
			_, err := s.Group().Create(ctx, models.Group{Name: "prod-vms", Filter: "memory > 0"})
			Expect(err).NotTo(HaveOccurred())
			_, err = s.Group().Create(ctx, models.Group{Name: "staging-vms", Filter: "memory > 0"})
			Expect(err).NotTo(HaveOccurred())

			f, err := filter.ParseWithGroupMap([]byte("name = 'prod-vms'"))
			Expect(err).NotTo(HaveOccurred())

			count, err := s.Group().Count(ctx, f)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(1))
		})
	})

	Context("Get", func() {
		It("should return ResourceNotFoundError when group does not exist", func() {
			_, err := s.Group().Get(ctx, 999)

			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("should return existing group", func() {
			// Arrange
			g := models.Group{Name: "testgroup", Filter: "memory >= 16GB", Description: "Test description"}
			created, err := s.Group().Create(ctx, g)
			Expect(err).NotTo(HaveOccurred())

			// Act
			retrieved, err := s.Group().Get(ctx, created.ID)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.ID).To(Equal(created.ID))
			Expect(retrieved.Name).To(Equal("testgroup"))
			Expect(retrieved.Filter).To(Equal("memory >= 16GB"))
			Expect(retrieved.Description).To(Equal("Test description"))
		})
	})

	Context("Create", func() {
		It("should create group and return with ID and timestamps", func() {
			// Arrange
			g := models.Group{Name: "newgroup", Filter: "cluster in ['prod', 'staging']", Description: "Production clusters"}

			// Act
			created, err := s.Group().Create(ctx, g)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(created.ID).To(BeNumerically(">", 0))
			Expect(created.Name).To(Equal("newgroup"))
			Expect(created.Filter).To(Equal("cluster in ['prod', 'staging']"))
			Expect(created.Description).To(Equal("Production clusters"))
			Expect(created.CreatedAt).NotTo(BeZero())
			Expect(created.UpdatedAt).NotTo(BeZero())
		})

		It("should create multiple groups with unique IDs", func() {
			g1 := models.Group{Name: "group1", Filter: "filter1"}
			g2 := models.Group{Name: "group2", Filter: "filter2"}

			created1, err := s.Group().Create(ctx, g1)
			Expect(err).NotTo(HaveOccurred())

			created2, err := s.Group().Create(ctx, g2)
			Expect(err).NotTo(HaveOccurred())

			Expect(created1.ID).NotTo(Equal(created2.ID))
		})

		It("should return DuplicateResourceError when creating group with duplicate name", func() {
			g := models.Group{Name: "duplicate-name", Filter: "filter1"}
			_, err := s.Group().Create(ctx, g)
			Expect(err).NotTo(HaveOccurred())

			// Try to create another group with the same name
			g2 := models.Group{Name: "duplicate-name", Filter: "filter2"}
			_, err = s.Group().Create(ctx, g2)
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsDuplicateResourceError(err)).To(BeTrue())
		})
	})

	Context("Update", func() {
		It("should return ResourceNotFoundError when group does not exist", func() {
			g := models.Group{Name: "updated"}
			_, err := s.Group().Update(ctx, 999, g)

			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("should update group name", func() {
			// Arrange
			g := models.Group{Name: "original", Filter: "original filter"}
			created, err := s.Group().Create(ctx, g)
			Expect(err).NotTo(HaveOccurred())

			// Act
			update := models.Group{Name: "updated", Filter: "original filter"}
			updated, err := s.Group().Update(ctx, created.ID, update)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Name).To(Equal("updated"))
			Expect(updated.Filter).To(Equal("original filter"))
		})

		It("should update group filter", func() {
			// Arrange
			g := models.Group{Name: "mygroup", Filter: "old filter"}
			created, err := s.Group().Create(ctx, g)
			Expect(err).NotTo(HaveOccurred())

			// Act
			update := models.Group{Name: "mygroup", Filter: "new filter"}
			updated, err := s.Group().Update(ctx, created.ID, update)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Name).To(Equal("mygroup"))
			Expect(updated.Filter).To(Equal("new filter"))
		})

		It("should update both name and filter", func() {
			// Arrange
			g := models.Group{Name: "original", Filter: "original"}
			created, err := s.Group().Create(ctx, g)
			Expect(err).NotTo(HaveOccurred())

			// Act
			update := models.Group{Name: "newname", Filter: "newfilter"}
			updated, err := s.Group().Update(ctx, created.ID, update)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Name).To(Equal("newname"))
			Expect(updated.Filter).To(Equal("newfilter"))
		})

		It("should update description", func() {
			// Arrange
			g := models.Group{Name: "mygroup", Filter: "filter", Description: "original description"}
			created, err := s.Group().Create(ctx, g)
			Expect(err).NotTo(HaveOccurred())

			// Act
			update := models.Group{Name: "mygroup", Filter: "filter", Description: "updated description"}
			updated, err := s.Group().Update(ctx, created.ID, update)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Description).To(Equal("updated description"))
		})

		It("should update UpdatedAt timestamp", func() {
			// Arrange
			g := models.Group{Name: "mygroup", Filter: "filter"}
			created, err := s.Group().Create(ctx, g)
			Expect(err).NotTo(HaveOccurred())

			// Act
			update := models.Group{Name: "updated", Filter: "filter"}
			updated, err := s.Group().Update(ctx, created.ID, update)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.UpdatedAt).To(BeTemporally(">=", created.UpdatedAt))
		})

		It("should return DuplicateResourceError when updating to existing name", func() {
			// Arrange: create two groups
			g1 := models.Group{Name: "first-group", Filter: "filter1"}
			created1, err := s.Group().Create(ctx, g1)
			Expect(err).NotTo(HaveOccurred())

			g2 := models.Group{Name: "second-group", Filter: "filter2"}
			_, err = s.Group().Create(ctx, g2)
			Expect(err).NotTo(HaveOccurred())

			// Act: try to update first group to have the same name as second
			update := models.Group{Name: "second-group", Filter: "filter1"}
			_, err = s.Group().Update(ctx, created1.ID, update)

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsDuplicateResourceError(err)).To(BeTrue())
		})
	})

	Context("Delete", func() {
		It("should return ResourceNotFoundError when group does not exist", func() {
			err := s.Group().Delete(ctx, 999)

			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("should delete existing group", func() {
			// Arrange
			g := models.Group{Name: "todelete", Filter: "filter"}
			created, err := s.Group().Create(ctx, g)
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = s.Group().Delete(ctx, created.ID)

			// Assert
			Expect(err).NotTo(HaveOccurred())

			// Verify group no longer exists
			_, err = s.Group().Get(ctx, created.ID)
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("should not affect other groups when deleting", func() {
			// Arrange
			g1 := models.Group{Name: "group1", Filter: "filter1"}
			g2 := models.Group{Name: "group2", Filter: "filter2"}
			created1, err := s.Group().Create(ctx, g1)
			Expect(err).NotTo(HaveOccurred())
			created2, err := s.Group().Create(ctx, g2)
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = s.Group().Delete(ctx, created1.ID)
			Expect(err).NotTo(HaveOccurred())

			// Assert
			groups, err := s.Group().List(ctx, nil, 0, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(groups).To(HaveLen(1))
			Expect(groups[0].ID).To(Equal(created2.ID))
		})
	})
})
