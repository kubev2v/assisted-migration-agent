package services_test

import (
	"context"
	"database/sql"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"

	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("VMService", func() {
	var (
		ctx context.Context
		db  *sql.DB
		st  *store.Store
		srv *services.VMService
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(":memory:")
		Expect(err).NotTo(HaveOccurred())

		st = store.NewStore(db, test.NewMockValidator())
		Expect(st.Migrate(ctx)).To(Succeed())
		Expect(test.InsertVMs(ctx, db)).To(Succeed())

		srv = services.NewVMService(st)
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	Context("Get", func() {
		// Given a VM exists in the database with ID "vm-001"
		// When we retrieve it by ID
		// Then it should return the full VM details
		It("should return a VM by ID", func() {
			// Act
			vm, err := srv.Get(ctx, "vm-001")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(vm).NotTo(BeNil())
			Expect(vm.ID).To(Equal("vm-001"))
			Expect(vm.Name).To(Equal("web-server-1"))
			Expect(vm.PowerState).To(Equal("poweredOn"))
			Expect(vm.ConnectionState).To(Equal("connected"))
			Expect(vm.CpuCount).To(Equal(int32(2)))
		})

		// Given no VM exists with the requested ID
		// When we retrieve it by ID
		// Then it should return a ResourceNotFoundError
		It("should return not found for non-existent VM", func() {
			// Act
			vm, err := srv.Get(ctx, "vm-nonexistent")

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
			Expect(vm).To(BeNil())
		})
	})

	Context("List", func() {
		// Given 10 VMs exist in the database
		// When we list without any filters
		// Then it should return all VMs with the correct total count
		It("should return all VMs with total count", func() {
			// Act
			vms, total, err := srv.List(ctx, services.VMListParams{})

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(10))
			Expect(vms).To(HaveLen(10))
		})

		// Given 10 VMs exist in the database
		// When we list with limit 3 and offset 0
		// Then it should return 3 VMs but total should still be 10
		It("should apply pagination", func() {
			// Arrange
			params := services.VMListParams{Limit: 3, Offset: 0}

			// Act
			vms, total, err := srv.List(ctx, params)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(10))
			Expect(vms).To(HaveLen(3))
		})

		// Given VMs in both "production" and "staging" clusters
		// When we list with an expression filter for cluster = 'production'
		// Then it should return only production VMs
		It("should filter by expression", func() {
			// Arrange
			params := services.VMListParams{
				Expression: "cluster = 'production'",
			}

			// Act
			vms, total, err := srv.List(ctx, params)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(4))
			for _, vm := range vms {
				Expect(vm.Cluster).To(Equal("production"))
			}
		})

		// Given VMs with different names
		// When we list sorted by name ascending
		// Then the results should be in alphabetical order
		It("should sort results", func() {
			// Arrange
			params := services.VMListParams{
				Sort: []services.SortField{{Field: "name", Desc: false}},
			}

			// Act
			vms, _, err := srv.List(ctx, params)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(len(vms)).To(BeNumerically(">", 1))
			for i := 1; i < len(vms); i++ {
				Expect(vms[i].Name >= vms[i-1].Name).To(BeTrue(),
					"expected %s >= %s", vms[i].Name, vms[i-1].Name)
			}
		})

		// Given VMs with different memory sizes
		// When we list sorted by memory descending with limit 1
		// Then it should return the VM with the most memory
		It("should combine sort and pagination", func() {
			// Arrange
			params := services.VMListParams{
				Sort:  []services.SortField{{Field: "memory", Desc: true}},
				Limit: 1,
			}

			// Act
			vms, total, err := srv.List(ctx, params)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(10))
			Expect(vms).To(HaveLen(1))
			Expect(vms[0].Memory).To(Equal(int32(16384)))
		})

		// Given VMs with different power states
		// When we list with offset beyond the total count
		// Then it should return an empty list with the correct total
		It("should return empty list for offset beyond total", func() {
			// Arrange
			params := services.VMListParams{Limit: 10, Offset: 100}

			// Act
			vms, total, err := srv.List(ctx, params)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(10))
			Expect(vms).To(BeEmpty())
		})
	})
})
