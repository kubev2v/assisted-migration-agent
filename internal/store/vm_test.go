package store_test

import (
	"context"
	"database/sql"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("VMStore", func() {
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

		s = store.NewStore(db)
	})

	AfterEach(func() {
		if db != nil {
			db.Close()
		}
	})

	Describe("List", func() {
		BeforeEach(func() {
			vms := []models.VM{
				{ID: "vm-1", Name: "web-server-1", State: "running", Datacenter: "dc1", Cluster: "cluster-a", DiskSize: 100, Memory: 4096},
				{ID: "vm-2", Name: "web-server-2", State: "running", Datacenter: "dc1", Cluster: "cluster-a", DiskSize: 200, Memory: 8192},
				{ID: "vm-3", Name: "db-server-1", State: "stopped", Datacenter: "dc1", Cluster: "cluster-b", DiskSize: 500, Memory: 16384},
				{ID: "vm-4", Name: "app-server-1", State: "running", Datacenter: "dc2", Cluster: "cluster-c", DiskSize: 150, Memory: 8192},
				{ID: "vm-5", Name: "app-server-2", State: "error", Datacenter: "dc2", Cluster: "cluster-c", DiskSize: 150, Memory: 32768},
			}
			err := s.VM().Insert(ctx, vms...)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return all VMs without filters", func() {
			vms, err := s.VM().List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(vms).To(HaveLen(5))
		})

		Context("ByDatacenters", func() {
			It("should filter by single datacenter", func() {
				vms, err := s.VM().List(ctx, store.ByDatacenters("dc1"))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(3))
				for _, vm := range vms {
					Expect(vm.Datacenter).To(Equal("dc1"))
				}
			})

			It("should filter by multiple datacenters (OR)", func() {
				vms, err := s.VM().List(ctx, store.ByDatacenters("dc1", "dc2"))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(5))
			})

			It("should return empty when datacenter not found", func() {
				vms, err := s.VM().List(ctx, store.ByDatacenters("dc-nonexistent"))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(BeEmpty())
			})
		})

		Context("ByClusters", func() {
			It("should filter by single cluster", func() {
				vms, err := s.VM().List(ctx, store.ByClusters("cluster-a"))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2))
				for _, vm := range vms {
					Expect(vm.Cluster).To(Equal("cluster-a"))
				}
			})

			It("should filter by multiple clusters (OR)", func() {
				vms, err := s.VM().List(ctx, store.ByClusters("cluster-a", "cluster-b"))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(3))
			})
		})

		Context("ByStatus", func() {
			It("should filter by single status", func() {
				vms, err := s.VM().List(ctx, store.ByStatus("running"))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(3))
				for _, vm := range vms {
					Expect(vm.State).To(Equal("running"))
				}
			})

			It("should filter by multiple statuses (OR)", func() {
				vms, err := s.VM().List(ctx, store.ByStatus("running", "stopped"))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(4))
			})
		})

		Context("ByDiskSizeRange", func() {
			It("should filter by disk size range", func() {
				vms, err := s.VM().List(ctx, store.ByDiskSizeRange(100, 200))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(3))
				for _, vm := range vms {
					Expect(vm.DiskSize).To(BeNumerically(">=", 100))
					Expect(vm.DiskSize).To(BeNumerically("<", 200))
				}
			})

			It("should return empty when no VMs in range", func() {
				vms, err := s.VM().List(ctx, store.ByDiskSizeRange(1000, 2000))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(BeEmpty())
			})
		})

		Context("ByMemorySizeRange", func() {
			It("should filter by memory size range", func() {
				vms, err := s.VM().List(ctx, store.ByMemorySizeRange(8000, 20000))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(3))
				for _, vm := range vms {
					Expect(vm.Memory).To(BeNumerically(">=", 8000))
					Expect(vm.Memory).To(BeNumerically("<", 20000))
				}
			})
		})

		Context("WithLimit and WithOffset", func() {
			It("should limit results", func() {
				vms, err := s.VM().List(ctx, store.WithLimit(2))
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2))
			})

			It("should offset results", func() {
				firstPage, err := s.VM().List(ctx, store.WithDefaultSort(), store.WithLimit(2))
				Expect(err).NotTo(HaveOccurred())
				Expect(firstPage).To(HaveLen(2))

				secondPage, err := s.VM().List(ctx, store.WithDefaultSort(), store.WithOffset(2), store.WithLimit(2))
				Expect(err).NotTo(HaveOccurred())
				Expect(secondPage).To(HaveLen(2))

				for _, vm := range secondPage {
					Expect(vm.ID).NotTo(Equal(firstPage[0].ID))
					Expect(vm.ID).NotTo(Equal(firstPage[1].ID))
				}
			})
		})

		Context("combined filters", func() {
			It("should combine datacenter and status filters (AND)", func() {
				vms, err := s.VM().List(ctx,
					store.ByDatacenters("dc1"),
					store.ByStatus("running"),
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2))
				for _, vm := range vms {
					Expect(vm.Datacenter).To(Equal("dc1"))
					Expect(vm.State).To(Equal("running"))
				}
			})

			It("should combine cluster and memory range filters", func() {
				vms, err := s.VM().List(ctx,
					store.ByClusters("cluster-a"),
					store.ByMemorySizeRange(4000, 10000),
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(2))
			})

			It("should combine multiple filters with pagination", func() {
				vms, err := s.VM().List(ctx,
					store.ByStatus("running"),
					store.WithLimit(1),
					store.WithOffset(1),
				)
				Expect(err).NotTo(HaveOccurred())
				Expect(vms).To(HaveLen(1))
			})
		})
	})

	Describe("Count", func() {
		BeforeEach(func() {
			vms := []models.VM{
				{ID: "vm-1", Name: "vm1", State: "running", Datacenter: "dc1", Cluster: "cluster-a", DiskSize: 100, Memory: 4096},
				{ID: "vm-2", Name: "vm2", State: "running", Datacenter: "dc1", Cluster: "cluster-a", DiskSize: 200, Memory: 8192},
				{ID: "vm-3", Name: "vm3", State: "stopped", Datacenter: "dc2", Cluster: "cluster-b", DiskSize: 500, Memory: 16384},
			}
			err := s.VM().Insert(ctx, vms...)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should count all VMs without filters", func() {
			count, err := s.VM().Count(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(3))
		})

		It("should count VMs with filter", func() {
			count, err := s.VM().Count(ctx, store.ByStatus("running"))
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(2))
		})
	})
})
