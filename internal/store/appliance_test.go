package store_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	sq "github.com/Masterminds/squirrel"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	vmfilter "github.com/kubev2v/assisted-migration-agent/internal/filter"
	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("ApplianceStore", Ordered, func() {
	var (
		ctx    context.Context
		s      *store.Store
		pool   *store.Pool
		tmpDir string
	)

	BeforeAll(func() {
		ctx = context.Background()
		var err error
		tmpDir, err = os.MkdirTemp("", "appliance-store-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)
		collDB, dbErr := pool.NewDatabase("coll", filepath.Join(tmpDir, "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(dbErr).NotTo(HaveOccurred())

		Expect(collDB.Migrate(ctx, func(mCtx context.Context, sqlDB *sql.DB) error {
			st, stErr := collDB.Store()
			if stErr != nil {
				return stErr
			}
			if pErr := duckdb_parser.New(st.Querier(), test.NewMockValidator()).Init(); pErr != nil {
				return pErr
			}
			return migrations.RunCollection(mCtx, sqlDB, "collection")
		})).To(Succeed())

		pool.Add(collDB)

		s, err = collDB.Store()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		if pool != nil {
			pool.Close()
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	insertVM := func(id, name string) {
		_, err := s.Querier().ExecContext(ctx, `
			INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory", "Template", "guest_apps")
			VALUES (?, ?, 'poweredOn', 'cluster-a', 4096, false, '[]')
		`, id, name)
		Expect(err).NotTo(HaveOccurred())
	}

	insertProduct := func(vmID string, key int, name, vendor, version string) {
		_, err := s.Querier().ExecContext(ctx, `
			INSERT INTO vm_vapp_products (vm_id, key, name, vendor, version)
			VALUES (?, ?, ?, ?, ?)
		`, vmID, key, name, vendor, version)
		Expect(err).NotTo(HaveOccurred())
	}

	findAppliance := func(appliances []models.Appliance, name string) models.Appliance {
		for _, a := range appliances {
			if a.Name == name {
				return a
			}
		}
		Fail("appliance not found: " + name)
		return models.Appliance{}
	}

	Context("Given an empty collection database", func() {
		When("appliances are listed", func() {
			It("should return no appliances", func() {
				// Act
				appliances, err := s.Appliance().List(ctx, nil)

				// Assert
				Expect(err).NotTo(HaveOccurred())
				Expect(appliances).To(BeEmpty())
			})
		})
	})

	Context("Given a collection seeded with vApp products", func() {
		BeforeEach(func() {
			insertVM("vm-1", "edge-01")
			insertVM("vm-2", "edge-02")

			insertProduct("vm-1", 1, "DataSync", "AWS", "1.0")
			insertProduct("vm-1", 2, "DataSync", "AWS", "2.0")
			insertProduct("vm-2", 1, "DataSync", "AWS", "2.0")
			insertProduct("vm-1", 3, "BIG-IP", "F5", "17.1")
			insertProduct("vm-2", 4, "BIG-IP", "F5", "17.1")
			insertProduct("vm-orphan", 1, "Ghost", "AWS", "2.0")

			_, err := s.Querier().ExecContext(ctx, `
				INSERT INTO vm_vapp_products (vm_id, key, name, vendor, version)
				VALUES
					('vm-1', 5, NULL, 'AWS', NULL),
					('vm-1', 6, '', 'AWS', NULL),
					('vm-1', 7, 'Bare', NULL, NULL)
			`)
			Expect(err).NotTo(HaveOccurred())

			_, err = s.Querier().ExecContext(ctx, `
				INSERT INTO vm_vapp_products (vm_id, key, name, vendor, version, full_version, vendor_url, product_url, app_url)
				VALUES ('vm-1', 8, 'Meta', 'AWS', '2.0', '2.0-build-7', 'https://example.test/vendor', 'https://example.test/product', 'https://example.test/app')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			_, err := s.Querier().ExecContext(ctx, `DELETE FROM vm_vapp_products`)
			Expect(err).NotTo(HaveOccurred())

			_, err = s.Querier().ExecContext(ctx, `DELETE FROM vinfo`)
			Expect(err).NotTo(HaveOccurred())
		})

		When("appliances are listed", func() {
			It("should return one appliance per product sorted by name", func() {
				// Act
				appliances, err := s.Appliance().List(ctx, nil)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				names := make([]string, 0, len(appliances))
				for _, a := range appliances {
					names = append(names, a.Name)
				}
				Expect(names).To(Equal([]string{"BIG-IP", "Bare", "DataSync", "Ghost", "Meta"}))
			})
		})

		When("the same product runs on multiple VMs", func() {
			It("should group them into a single appliance with all VMs", func() {
				// Act
				appliances, err := s.Appliance().List(ctx, nil)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				dataSync := findAppliance(appliances, "DataSync")
				Expect(dataSync.Vendor).To(Equal("AWS"))
				Expect(dataSync.VirtualMachines).To(HaveLen(2))
				Expect(dataSync.VirtualMachines).To(HaveKeyWithValue("vm-1", "edge-01"))
				Expect(dataSync.VirtualMachines).To(HaveKeyWithValue("vm-2", "edge-02"))
			})
		})

		When("a VM reports the same product under different keys", func() {
			It("should list the VM once and use the lowest key row metadata", func() {
				// Act
				appliances, err := s.Appliance().List(ctx, nil)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				dataSync := findAppliance(appliances, "DataSync")
				Expect(dataSync.Version).To(Equal("1.0"))
				Expect(dataSync.VirtualMachines).To(HaveLen(2))
			})
		})

		When("product rows lack a name", func() {
			It("should skip NULL and empty names", func() {
				// Act
				appliances, err := s.Appliance().List(ctx, nil)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				Expect(appliances).To(HaveLen(5))
			})
		})

		When("vendor and version are missing", func() {
			It("should default vendor to Unknown and version to empty", func() {
				// Act
				appliances, err := s.Appliance().List(ctx, nil)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				bare := findAppliance(appliances, "Bare")
				Expect(bare.Vendor).To(Equal("Unknown"))
				Expect(bare.Version).To(BeEmpty())
			})
		})

		When("full product metadata is reported", func() {
			It("should carry the metadata and default confidence to 100", func() {
				// Act
				appliances, err := s.Appliance().List(ctx, nil)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				meta := findAppliance(appliances, "Meta")
				Expect(meta.FullVersion).To(Equal("2.0-build-7"))
				Expect(meta.VendorUrl).To(Equal("https://example.test/vendor"))
				Expect(meta.ProductUrl).To(Equal("https://example.test/product"))
				Expect(meta.ApplicationUrl).To(Equal("https://example.test/app"))
				Expect(meta.Confidence).To(Equal(100))
			})
		})

		When("a product row has no matching inventory VM", func() {
			It("should keep the VM with an empty name", func() {
				// Act
				appliances, err := s.Appliance().List(ctx, nil)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				ghost := findAppliance(appliances, "Ghost")
				Expect(ghost.VirtualMachines).To(HaveLen(1))
				Expect(ghost.VirtualMachines).To(HaveKeyWithValue("vm-orphan", ""))
			})
		})

		When("filtered by vm_id", func() {
			It("should return only appliances running on that VM", func() {
				// Act
				appliances, err := s.Appliance().List(ctx, sq.Eq{"vm_id": "vm-1"})
				Expect(err).NotTo(HaveOccurred())

				// Assert
				Expect(appliances).To(HaveLen(4))
				dataSync := findAppliance(appliances, "DataSync")
				Expect(dataSync.VirtualMachines).To(HaveLen(1))
				Expect(dataSync.VirtualMachines).To(HaveKeyWithValue("vm-1", "edge-01"))
				bigIP := findAppliance(appliances, "BIG-IP")
				Expect(bigIP.VirtualMachines).To(HaveLen(1))
			})
		})

		When("filtered by a vm_id list expression", func() {
			It("should return appliances running on any of the given VMs", func() {
				// Arrange
				applianceFilter, err := vmfilter.ParseWithApplianceMap([]byte("vm_id in ['vm-1', 'vm-2']"))
				Expect(err).NotTo(HaveOccurred())

				// Act
				appliances, err := s.Appliance().List(ctx, applianceFilter)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				Expect(appliances).To(HaveLen(4))
				dataSync := findAppliance(appliances, "DataSync")
				Expect(dataSync.VirtualMachines).To(HaveLen(2))
				Expect(dataSync.VirtualMachines).To(HaveKeyWithValue("vm-1", "edge-01"))
				Expect(dataSync.VirtualMachines).To(HaveKeyWithValue("vm-2", "edge-02"))
			})
		})

		When("filtered by a name expression", func() {
			It("should return only the matching appliance", func() {
				// Arrange
				applianceFilter, err := vmfilter.ParseWithApplianceMap([]byte("name = 'DataSync'"))
				Expect(err).NotTo(HaveOccurred())

				// Act
				appliances, err := s.Appliance().List(ctx, applianceFilter)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				Expect(appliances).To(HaveLen(1))
				Expect(appliances[0].Name).To(Equal("DataSync"))
			})
		})

		When("filtered by a vendor expression", func() {
			It("should return only appliances from that vendor", func() {
				// Arrange
				applianceFilter, err := vmfilter.ParseWithApplianceMap([]byte("vendor = 'F5'"))
				Expect(err).NotTo(HaveOccurred())

				// Act
				appliances, err := s.Appliance().List(ctx, applianceFilter)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				Expect(appliances).To(HaveLen(1))
				Expect(appliances[0].Name).To(Equal("BIG-IP"))
			})
		})

		When("filtered by a version expression", func() {
			It("should return only appliances with that version", func() {
				// Arrange
				applianceFilter, err := vmfilter.ParseWithApplianceMap([]byte("version = '17.1'"))
				Expect(err).NotTo(HaveOccurred())

				// Act
				appliances, err := s.Appliance().List(ctx, applianceFilter)
				Expect(err).NotTo(HaveOccurred())

				// Assert
				Expect(appliances).To(HaveLen(1))
				Expect(appliances[0].Name).To(Equal("BIG-IP"))
			})
		})
	})
})
