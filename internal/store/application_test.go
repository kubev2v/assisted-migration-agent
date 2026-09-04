package store_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("ApplicationStore", func() {
	var (
		ctx    context.Context
		s      *store.Store
		pool   *store.Pool
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		tmpDir, err = os.MkdirTemp("", "app-store-test-*")
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

	AfterEach(func() {
		if pool != nil {
			pool.Close()
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	It("should insert records and list overviews", func() {
		records := []models.ApplicationVMRecord{
			{AppName: "PostgreSQL", AppDesc: "PG Servers", VMID: "vm-1", VMName: "db-01"},
			{AppName: "PostgreSQL", AppDesc: "PG Servers", VMID: "vm-2", VMName: "db-02"},
			{AppName: "Apache", AppDesc: "Web Servers", VMID: "vm-3", VMName: "web-01"},
		}

		Expect(s.Application().ReplaceAll(ctx, records)).To(Succeed())

		overviews, err := s.Application().ListOverviews(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(overviews).To(HaveLen(2))

		Expect(overviews[0].Name).To(Equal("Apache"))
		Expect(overviews[0].VMCount).To(Equal(1))

		Expect(overviews[1].Name).To(Equal("PostgreSQL"))
		Expect(overviews[1].VMCount).To(Equal(2))
		Expect(overviews[1].VMs[0].ID).To(Equal("vm-1"))
		Expect(overviews[1].VMs[1].ID).To(Equal("vm-2"))
	})

	It("should clear previous data on replace", func() {
		initial := []models.ApplicationVMRecord{
			{AppName: "OldApp", AppDesc: "Old", VMID: "vm-1", VMName: "old-vm"},
		}
		Expect(s.Application().ReplaceAll(ctx, initial)).To(Succeed())

		updated := []models.ApplicationVMRecord{
			{AppName: "NewApp", AppDesc: "New", VMID: "vm-2", VMName: "new-vm"},
		}
		Expect(s.Application().ReplaceAll(ctx, updated)).To(Succeed())

		overviews, err := s.Application().ListOverviews(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(overviews).To(HaveLen(1))
		Expect(overviews[0].Name).To(Equal("NewApp"))
	})

	It("should clear table when replacing with empty records", func() {
		Expect(s.Application().ReplaceAll(ctx, []models.ApplicationVMRecord{
			{AppName: "App", AppDesc: "Desc", VMID: "vm-1", VMName: "vm"},
		})).To(Succeed())

		Expect(s.Application().ReplaceAll(ctx, nil)).To(Succeed())

		overviews, err := s.Application().ListOverviews(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(overviews).To(BeNil())
	})

	It("should return nil for empty table", func() {
		overviews, err := s.Application().ListOverviews(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(overviews).To(BeNil())
	})
})
