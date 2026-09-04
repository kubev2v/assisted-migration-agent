package services_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	svc "github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

// buildTestStore creates a temporary DuckDB populated with the given VMs.
// migratable=true inserts no Critical concern; migratable=false inserts one Critical concern row.
// The caller is responsible for cleanup (os.RemoveAll of the returned tmpDir).
func buildTestStore(vms []struct {
	id         string
	cluster    string
	migratable bool
}) (st *store.Store, tmpDir string) {
	var err error
	tmpDir, err = os.MkdirTemp("", "cmp-test-*")
	Expect(err).NotTo(HaveOccurred())

	pool := store.NewPool(5 * time.Minute)
	database, err := pool.NewDatabase(
		"test",
		filepath.Join(tmpDir, "test.duckdb"),
		time.Now(),
		store.EagerConnectionInitilization,
		0,
		store.ReadWriteDatabase,
	)
	Expect(err).NotTo(HaveOccurred())

	st, err = database.Store()
	Expect(err).NotTo(HaveOccurred())
	Expect(duckdb_parser.New(st.Querier(), nil).Init()).To(Succeed())

	ctx := context.Background()
	Expect(database.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
		return migrations.RunCollection(ctx, db, "test")
	})).To(Succeed())

	// Insert VMs directly via SQL using the vinfo table's required columns.
	for _, vm := range vms {
		_, err := st.Querier().ExecContext(ctx,
			`INSERT INTO vinfo ("VM ID", "VM", "Cluster", "Powerstate", "Template", "Memory", "CPUs")
             VALUES (?, ?, ?, 'poweredOn', false, 1024, 2)`,
			vm.id, vm.id, vm.cluster,
		)
		Expect(err).NotTo(HaveOccurred())

		if !vm.migratable {
			_, err = st.Querier().ExecContext(ctx,
				`INSERT INTO concerns ("VM_ID", "Concern_ID", "Label", "Category", "Assessment")
                 VALUES (?, ?, 'Critical issue', 'Critical', 'Must fix before migration')`,
				vm.id, "concern-"+vm.id,
			)
			Expect(err).NotTo(HaveOccurred())
		}
	}

	return st, tmpDir
}

var _ = Describe("ComparisonService", func() {
	ctx := context.Background()

	Describe("Summary", func() {
		It("returns correct aggregates and deltas for two collections", func() {
			// Collection A: vm-1 (migratable, cluster-A), vm-2 (non-migratable, cluster-A)
			// Collection B: vm-1 (migratable, cluster-B), vm-3 (migratable, cluster-B)
			aVMs := []struct {
				id         string
				cluster    string
				migratable bool
			}{
				{"vm-1", "cluster-A", true},
				{"vm-2", "cluster-A", false},
			}
			bVMs := []struct {
				id         string
				cluster    string
				migratable bool
			}{
				{"vm-1", "cluster-B", true},
				{"vm-3", "cluster-B", true},
			}

			storeA, tmpDirA := buildTestStore(aVMs)
			storeB, tmpDirB := buildTestStore(bVMs)
			DeferCleanup(func() { os.RemoveAll(tmpDirA); os.RemoveAll(tmpDirB) }) //nolint:errcheck

			metaA := models.CollectionMeta{ID: "collection-a", CreatedAt: time.Now()}
			metaB := models.CollectionMeta{ID: "collection-b", CreatedAt: time.Now().Add(time.Hour)}

			service := svc.NewComparisonService(storeA, storeB, metaA, metaB)
			summary, err := service.Summary(ctx)

			Expect(err).ToNot(HaveOccurred())

			// Aggregates for A
			Expect(summary.A.ID).To(Equal("collection-a"))
			Expect(summary.A.TotalVMs).To(Equal(2))
			Expect(summary.A.Migratable).To(Equal(1))
			Expect(summary.A.NonMigratable).To(Equal(1))
			Expect(summary.A.Clusters).To(Equal(1))

			// Aggregates for B
			Expect(summary.B.TotalVMs).To(Equal(2))
			Expect(summary.B.Migratable).To(Equal(2))
			Expect(summary.B.NonMigratable).To(Equal(0))
			Expect(summary.B.Clusters).To(Equal(1))

			// Diffs (B - A)
			Expect(summary.TotalVMs.Delta).To(Equal(0))   // 2 - 2
			Expect(summary.TotalVMs.OnlyInA).To(Equal(1)) // vm-2 not in B
			Expect(summary.TotalVMs.OnlyInB).To(Equal(1)) // vm-3 not in A

			Expect(summary.Migratable.Delta).To(Equal(1))   // 2 - 1
			Expect(summary.Migratable.OnlyInA).To(Equal(0)) // no VM migratable in A but not in B
			Expect(summary.Migratable.OnlyInB).To(Equal(1)) // vm-3 migratable in B, not in A

			Expect(summary.NonMigratable.Delta).To(Equal(-1))  // 0 - 1
			Expect(summary.NonMigratable.OnlyInA).To(Equal(1)) // vm-2 non-migratable in A, not in B
			Expect(summary.NonMigratable.OnlyInB).To(Equal(0))

			// Clusters has no onlyInA/onlyInB
			Expect(summary.Clusters.Delta).To(Equal(0)) // 1 - 1
		})

		It("does not count excluded VMs in any aggregate or diff", func() {
			// Collection A: vm-excl (migratable, will be excluded), vm-1 (migratable), vm-2 (non-migratable)
			// Collection B: vm-1 (migratable), vm-3 (migratable)
			// After excluding vm-excl from A, effective A = {vm-1, vm-2}.
			aVMs := []struct {
				id         string
				cluster    string
				migratable bool
			}{
				{"vm-excl", "c", true},
				{"vm-1", "c", true},
				{"vm-2", "c", false},
			}
			bVMs := []struct {
				id         string
				cluster    string
				migratable bool
			}{
				{"vm-1", "c", true},
				{"vm-3", "c", true},
			}

			storeA, tmpDirA := buildTestStore(aVMs)
			storeB, tmpDirB := buildTestStore(bVMs)
			DeferCleanup(func() { os.RemoveAll(tmpDirA); os.RemoveAll(tmpDirB) }) //nolint:errcheck

			Expect(storeA.VM().UpdateMigrationExcluded(ctx, "vm-excl", true)).To(Succeed())

			metaA := models.CollectionMeta{ID: "a", CreatedAt: time.Now()}
			metaB := models.CollectionMeta{ID: "b", CreatedAt: time.Now()}
			service := svc.NewComparisonService(storeA, storeB, metaA, metaB)

			summary, err := service.Summary(ctx)
			Expect(err).ToNot(HaveOccurred())

			// vm-excl must not inflate A's totals
			Expect(summary.A.TotalVMs).To(Equal(2),
				"excluded VM must not count toward total")
			Expect(summary.A.Migratable).To(Equal(1),
				"excluded VM must not count toward migratable")

			// OnlyInA for total dimension: vm-2 (absent from B) — vm-excl must not appear
			Expect(summary.TotalVMs.OnlyInA).To(Equal(1))
			Expect(summary.TotalVMs.OnlyInB).To(Equal(1)) // vm-3

			// Verify via Diff that vm-excl ID is not present in any diff page
			diff, err := service.Diff(ctx, models.DimensionTotal, 1, 20)
			Expect(err).ToNot(HaveOccurred())
			Expect(diff.OnlyInA.VMIDs).NotTo(ContainElement("vm-excl"))
			Expect(diff.OnlyInB.VMIDs).NotTo(ContainElement("vm-excl"))
		})
	})

	Describe("Diff", func() {
		It("returns paginated VM IDs for the migratable dimension", func() {
			aVMs := []struct {
				id         string
				cluster    string
				migratable bool
			}{
				{"vm-1", "c", true},
				{"vm-2", "c", false},
			}
			bVMs := []struct {
				id         string
				cluster    string
				migratable bool
			}{
				{"vm-1", "c", true},
				{"vm-3", "c", true},
			}

			storeA, tmpDirA := buildTestStore(aVMs)
			storeB, tmpDirB := buildTestStore(bVMs)
			DeferCleanup(func() { os.RemoveAll(tmpDirA); os.RemoveAll(tmpDirB) }) //nolint:errcheck

			metaA := models.CollectionMeta{ID: "a", CreatedAt: time.Now()}
			metaB := models.CollectionMeta{ID: "b", CreatedAt: time.Now()}

			service := svc.NewComparisonService(storeA, storeB, metaA, metaB)
			diff, err := service.Diff(ctx, models.DimensionMigratable, 1, 20)

			Expect(err).ToNot(HaveOccurred())
			Expect(diff.Dimension).To(Equal(models.DimensionMigratable))

			// vm-1 is migratable in both — not in either list
			// vm-2 is non-migratable in A and absent in B — not in migratable lists
			// vm-3 is migratable in B but absent in A — goes to onlyInB
			Expect(diff.OnlyInA.Total).To(Equal(0))
			Expect(diff.OnlyInA.VMIDs).To(BeEmpty())
			Expect(diff.OnlyInB.Total).To(Equal(1))
			Expect(diff.OnlyInB.VMIDs).To(ConsistOf("vm-3"))
		})

		It("returns empty vmIds when page exceeds pageCount", func() {
			aVMs := []struct {
				id         string
				cluster    string
				migratable bool
			}{{"vm-1", "c", true}}
			bVMs := []struct {
				id         string
				cluster    string
				migratable bool
			}{}

			storeA, tmpDirA := buildTestStore(aVMs)
			storeB, tmpDirB := buildTestStore(bVMs)
			DeferCleanup(func() { os.RemoveAll(tmpDirA); os.RemoveAll(tmpDirB) }) //nolint:errcheck

			metaA := models.CollectionMeta{ID: "a", CreatedAt: time.Now()}
			metaB := models.CollectionMeta{ID: "b", CreatedAt: time.Now()}

			service := svc.NewComparisonService(storeA, storeB, metaA, metaB)
			diff, err := service.Diff(ctx, models.DimensionTotal, 99, 20)

			Expect(err).ToNot(HaveOccurred())
			// onlyInA has vm-1, but page 99 is beyond pageCount
			Expect(diff.OnlyInA.Total).To(Equal(1))
			Expect(diff.OnlyInA.VMIDs).To(BeEmpty())
		})

		It("places a VM that changed status into the correct onlyIn lists", func() {
			// vm-flip: migratable in A, non-migratable in B
			// vm-stable: migratable in both (should appear in neither diff list)
			aVMs := []struct {
				id         string
				cluster    string
				migratable bool
			}{
				{"vm-flip", "c", true},
				{"vm-stable", "c", true},
			}
			bVMs := []struct {
				id         string
				cluster    string
				migratable bool
			}{
				{"vm-flip", "c", false},
				{"vm-stable", "c", true},
			}

			storeA, tmpDirA := buildTestStore(aVMs)
			storeB, tmpDirB := buildTestStore(bVMs)
			DeferCleanup(func() {
				os.RemoveAll(tmpDirA) //nolint:errcheck
				os.RemoveAll(tmpDirB) //nolint:errcheck
			})
			metaA := models.CollectionMeta{ID: "a", CreatedAt: time.Now()}
			metaB := models.CollectionMeta{ID: "b", CreatedAt: time.Now()}

			service := svc.NewComparisonService(storeA, storeB, metaA, metaB)
			diff, err := service.Diff(ctx, models.DimensionMigratable, 1, 20)

			Expect(err).ToNot(HaveOccurred())
			// vm-flip is migratable in A but non-migratable in B → onlyInA
			Expect(diff.OnlyInA.VMIDs).To(ConsistOf("vm-flip"))
			// vm-stable is migratable in both → neither list
			Expect(diff.OnlyInB.VMIDs).To(BeEmpty())

			// Also verify via Summary: total diff should be 0 (2 VMs each),
			// but migratable went from 2 to 1
			summary, err := service.Summary(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(summary.Migratable.OnlyInA).To(Equal(1)) // vm-flip
			Expect(summary.Migratable.OnlyInB).To(Equal(0))
			Expect(summary.NonMigratable.OnlyInA).To(Equal(0))
			Expect(summary.NonMigratable.OnlyInB).To(Equal(1)) // vm-flip
		})

		It("returns pageCount 0 when there are no results in a diff set", func() {
			// A has one migratable VM; B has the same VM migratable.
			// MigratableOnlyInA is empty (vm-1 is migratable in both).
			aVMs := []struct {
				id         string
				cluster    string
				migratable bool
			}{{"vm-1", "c", true}}
			bVMs := []struct {
				id         string
				cluster    string
				migratable bool
			}{{"vm-1", "c", true}}

			storeA, tmpDirA := buildTestStore(aVMs)
			storeB, tmpDirB := buildTestStore(bVMs)
			DeferCleanup(func() { os.RemoveAll(tmpDirA); os.RemoveAll(tmpDirB) }) //nolint:errcheck

			metaA := models.CollectionMeta{ID: "a", CreatedAt: time.Now()}
			metaB := models.CollectionMeta{ID: "b", CreatedAt: time.Now()}
			service := svc.NewComparisonService(storeA, storeB, metaA, metaB)

			diff, err := service.Diff(ctx, models.DimensionMigratable, 1, 20)
			Expect(err).ToNot(HaveOccurred())

			// Both sides are empty — PageCount must be 0, not 1.
			Expect(diff.OnlyInA.Total).To(Equal(0))
			Expect(diff.OnlyInA.PageCount).To(Equal(0))
			Expect(diff.OnlyInA.VMIDs).To(BeEmpty())

			Expect(diff.OnlyInB.Total).To(Equal(0))
			Expect(diff.OnlyInB.PageCount).To(Equal(0))
			Expect(diff.OnlyInB.VMIDs).To(BeEmpty())
		})

		It("returns correct VM IDs for the total dimension", func() {
			// A: vm-1 (migratable), vm-2 (non-migratable)
			// B: vm-1 (migratable), vm-3 (migratable)
			// total OnlyInA: vm-2 (absent from B)
			// total OnlyInB: vm-3 (absent from A)
			aVMs := []struct {
				id         string
				cluster    string
				migratable bool
			}{
				{"vm-1", "c", true},
				{"vm-2", "c", false},
			}
			bVMs := []struct {
				id         string
				cluster    string
				migratable bool
			}{
				{"vm-1", "c", true},
				{"vm-3", "c", true},
			}

			storeA, tmpDirA := buildTestStore(aVMs)
			storeB, tmpDirB := buildTestStore(bVMs)
			DeferCleanup(func() { os.RemoveAll(tmpDirA); os.RemoveAll(tmpDirB) }) //nolint:errcheck

			metaA := models.CollectionMeta{ID: "a", CreatedAt: time.Now()}
			metaB := models.CollectionMeta{ID: "b", CreatedAt: time.Now()}
			service := svc.NewComparisonService(storeA, storeB, metaA, metaB)

			diff, err := service.Diff(ctx, models.DimensionTotal, 1, 20)

			Expect(err).ToNot(HaveOccurred())
			Expect(diff.Dimension).To(Equal(models.DimensionTotal))
			Expect(diff.OnlyInA.VMIDs).To(ConsistOf("vm-2"))
			Expect(diff.OnlyInA.Total).To(Equal(1))
			Expect(diff.OnlyInB.VMIDs).To(ConsistOf("vm-3"))
			Expect(diff.OnlyInB.Total).To(Equal(1))
		})

		It("returns correct VM IDs for the non-migratable dimension", func() {
			// vm-nm-a: non-migratable in A, absent from B → NonMigratableOnlyInA
			// vm-nm-b: non-migratable in B, absent from A → NonMigratableOnlyInB
			// vm-shared-nm: non-migratable in both → neither list
			aVMs := []struct {
				id         string
				cluster    string
				migratable bool
			}{
				{"vm-nm-a", "c", false},
				{"vm-shared-nm", "c", false},
			}
			bVMs := []struct {
				id         string
				cluster    string
				migratable bool
			}{
				{"vm-nm-b", "c", false},
				{"vm-shared-nm", "c", false},
			}

			storeA, tmpDirA := buildTestStore(aVMs)
			storeB, tmpDirB := buildTestStore(bVMs)
			DeferCleanup(func() { os.RemoveAll(tmpDirA); os.RemoveAll(tmpDirB) }) //nolint:errcheck

			metaA := models.CollectionMeta{ID: "a", CreatedAt: time.Now()}
			metaB := models.CollectionMeta{ID: "b", CreatedAt: time.Now()}
			service := svc.NewComparisonService(storeA, storeB, metaA, metaB)

			diff, err := service.Diff(ctx, models.DimensionNonMigratable, 1, 20)

			Expect(err).ToNot(HaveOccurred())
			Expect(diff.Dimension).To(Equal(models.DimensionNonMigratable))
			// vm-nm-a: non-mig in A, absent from B → OnlyInA
			Expect(diff.OnlyInA.VMIDs).To(ConsistOf("vm-nm-a"))
			Expect(diff.OnlyInA.Total).To(Equal(1))
			// vm-nm-b: non-mig in B, absent from A → OnlyInB
			Expect(diff.OnlyInB.VMIDs).To(ConsistOf("vm-nm-b"))
			Expect(diff.OnlyInB.Total).To(Equal(1))
		})
	})
})
