package store_test

import (
	"context"
	"database/sql"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("InspectionStore", func() {
	Context("vm_inspection_concerns", func() {
		var (
			ctx context.Context
			s   *store.Store
			db  *sql.DB
		)

		BeforeEach(func() {
			ctx = context.Background()

			var err error
			db, err = store.NewDB(nil, ":memory:")
			Expect(err).NotTo(HaveOccurred())

			err = migrations.Run(ctx, db)
			Expect(err).NotTo(HaveOccurred())

			_, err = db.ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM") VALUES ('vm-inspect-1', 'test-vm')
			`)
			Expect(err).NotTo(HaveOccurred())

			s = store.NewStore(db, test.NewMockValidator())
		})

		AfterEach(func() {
			if db != nil {
				_ = db.Close()
			}
		})

		It("should save concerns and read them back as one inspection run", func() {
			concerns := []models.VmInspectionConcern{
				{Category: "disk", Label: "Disk layout", Msg: "ok"},
				{Category: "network", Label: "NICs", Msg: "warning"},
			}
			err := s.WithTx(ctx, func(txCtx context.Context) error {
				return s.Inspection().InsertResult(txCtx, "vm-inspect-1", concerns)
			})
			Expect(err).NotTo(HaveOccurred())

			results, err := s.Inspection().ListResults(ctx, "vm-inspect-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(1))
			Expect(results[0].VMID).To(Equal("vm-inspect-1"))
			Expect(results[0].InspectionID).To(BeNumerically(">", 0))
			Expect(results[0].Concerns).To(HaveLen(2))
			byCat := make(map[string]string)
			byLabel := make(map[string]string)
			for _, c := range results[0].Concerns {
				byCat[c.Category] = c.Msg
				byLabel[c.Category] = c.Label
			}
			Expect(byCat["disk"]).To(Equal("ok"))
			Expect(byCat["network"]).To(Equal("warning"))
			Expect(byLabel["disk"]).To(Equal("Disk layout"))
			Expect(byLabel["network"]).To(Equal("NICs"))
		})

		It("should list multiple inspection runs newest first by inspection_id", func() {
			firstRun := []models.VmInspectionConcern{
				{Category: "stale", Label: "first-run", Msg: "from-first"},
			}
			err := s.WithTx(ctx, func(txCtx context.Context) error {
				return s.Inspection().InsertResult(txCtx, "vm-inspect-1", firstRun)
			})
			Expect(err).NotTo(HaveOccurred())

			secondRun := []models.VmInspectionConcern{
				{Category: "fresh", Label: "second-run", Msg: "from-second"},
				{Category: "network", Label: "n2", Msg: "extra"},
			}
			err = s.WithTx(ctx, func(txCtx context.Context) error {
				return s.Inspection().InsertResult(txCtx, "vm-inspect-1", secondRun)
			})
			Expect(err).NotTo(HaveOccurred())

			results, err := s.Inspection().ListResults(ctx, "vm-inspect-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(HaveLen(2))
			Expect(results[0].InspectionID).To(BeNumerically(">", results[1].InspectionID))

			Expect(results[0].Concerns).To(HaveLen(2))
			Expect(results[0].Concerns).To(ContainElement(models.VmInspectionConcern{Category: "fresh", Label: "second-run", Msg: "from-second"}))
			Expect(results[0].Concerns).To(ContainElement(models.VmInspectionConcern{Category: "network", Label: "n2", Msg: "extra"}))

			Expect(results[1].Concerns).To(HaveLen(1))
			Expect(results[1].Concerns[0]).To(Equal(models.VmInspectionConcern{Category: "stale", Label: "first-run", Msg: "from-first"}))
		})

		It("should return an empty list when the VM has no inspection results", func() {
			_, err := db.ExecContext(ctx, `
				INSERT INTO vinfo ("VM ID", "VM") VALUES ('vm-no-result', 'other')
			`)
			Expect(err).NotTo(HaveOccurred())

			results, err := s.Inspection().ListResults(ctx, "vm-no-result")
			Expect(err).NotTo(HaveOccurred())
			Expect(results).To(BeEmpty())
		})
	})
})
