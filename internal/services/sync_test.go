package services_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

// newSyncTestDB creates a migrated collection DuckDB database for sync tests.
func newSyncTestDB(pool *store.Pool, tmpDir, name string) (*store.Database, *store.Store) {
	db, err := pool.NewDatabase(name, filepath.Join(tmpDir, name+".duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	st, err := db.Store()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, duckdb_parser.New(st.Querier(), nil).Init()).To(Succeed())
	ExpectWithOffset(1, db.Migrate(context.Background(), func(ctx context.Context, sqlDb *sql.DB) error {
		return migrations.RunCollection(ctx, sqlDb, name)
	})).To(Succeed())

	return db, st
}

// insertSyncTestVM inserts a minimal VM row directly into vinfo.
func insertSyncTestVM(ctx context.Context, st *store.Store, vmID, name string) {
	_, err := st.Querier().ExecContext(ctx,
		`INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Cluster", "Memory", "Template", "Folder")
		 VALUES (?, ?, 'poweredOn', 'cluster-a', 4096, false, '/')`, vmID, name)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
}

var _ = Describe("Collection sync", func() {
	var (
		ctx    context.Context
		pool   *store.Pool
		tmpDir string
		prevDB *store.Database
		newDB  *store.Database
		prevSt *store.Store
	)

	const attachedSchema = "new_col"

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		tmpDir, err = os.MkdirTemp("", "sync-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)
		prevDB, prevSt = newSyncTestDB(pool, tmpDir, "prev")
		newDB, _ = newSyncTestDB(pool, tmpDir, "new")
	})

	AfterEach(func() {
		pool.Close()
		_ = os.RemoveAll(tmpDir)
	})

	// attachNew closes newDB and attaches it to prevSt as attachedSchema.
	// Returns a cleanup func that detaches and may be deferred.
	attachNew := func() func() {
		Expect(newDB.Close()).To(Succeed())
		Expect(prevSt.AttachDatabase(ctx, newDB, attachedSchema, store.ReadWriteDatabase)).To(Succeed())
		return func() {
			_ = prevSt.DetachDatabase(ctx, attachedSchema)
		}
	}

	// Suppress unused-variable warning: prevDB is used only to confirm the DB
	// was created; the attach machinery accesses it via newDB.
	_ = &prevDB

	Describe("syncAttached", func() {
		Context("groups", func() {
			It("copies group definitions preserving ID and created_at, setting updated_at to now", func() {
				groupID := uuid.New()
				createdAt := time.Now().Add(-24 * time.Hour).Truncate(time.Second)
				_, err := prevSt.Querier().ExecContext(ctx,
					`INSERT INTO groups (id, name, description, filter, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
					groupID, "prod-group", "desc", "cluster = 'prod'", createdAt, createdAt)
				Expect(err).NotTo(HaveOccurred())

				detach := attachNew()
				defer detach()

				syncTime := time.Now().Truncate(time.Second)
				Expect(services.SyncAttached(ctx, prevSt, attachedSchema, syncTime)).To(Succeed())
				detach()

				newSt, err := newDB.Store()
				Expect(err).NotTo(HaveOccurred())
				fetched, err := newSt.Group().Get(ctx, groupID)
				Expect(err).NotTo(HaveOccurred())
				Expect(fetched.ID).To(Equal(groupID))
				Expect(fetched.Name).To(Equal("prod-group"))
				Expect(fetched.CreatedAt.UTC().Truncate(time.Second)).To(Equal(createdAt.UTC()))
				Expect(fetched.UpdatedAt.UTC().Truncate(time.Second)).To(Equal(syncTime.UTC()))
				Expect(fetched.Inventory).To(BeNil())
			})

			It("is a no-op when the previous collection has no groups", func() {
				detach := attachNew()
				defer detach()
				Expect(services.SyncAttached(ctx, prevSt, attachedSchema, time.Now())).To(Succeed())
				detach()

				newSt, err := newDB.Store()
				Expect(err).NotTo(HaveOccurred())
				groups, err := newSt.Group().List(ctx, nil, 0, 0)
				Expect(err).NotTo(HaveOccurred())
				Expect(groups).To(BeEmpty())
			})
		})

		Context("VM labels", func() {
			It("copies user labels to matching VMs, excluding system labels", func() {
				insertSyncTestVM(ctx, prevSt, "vm-1", "alpha")

				newSt, err := newDB.Store()
				Expect(err).NotTo(HaveOccurred())
				insertSyncTestVM(ctx, newSt, "vm-1", "alpha")

				Expect(prevSt.VM().AddLabel(ctx, "vm-1", "prod")).To(Succeed())
				Expect(prevSt.VM().AddLabel(ctx, "vm-1", services.LabelNew)).To(Succeed())

				detach := attachNew()
				defer detach()
				Expect(services.SyncAttached(ctx, prevSt, attachedSchema, time.Now())).To(Succeed())
				detach()

				newSt, err = newDB.Store()
				Expect(err).NotTo(HaveOccurred())
				labelsMap, err := newSt.VM().ListLabels(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(labelsMap["vm-1"]).To(ConsistOf("prod"))
				Expect(labelsMap["vm-1"]).NotTo(ContainElement(services.LabelNew))
			})

			It("skips VMs that exist in previous but not in new collection", func() {
				insertSyncTestVM(ctx, prevSt, "vm-gone", "removed")
				Expect(prevSt.VM().AddLabel(ctx, "vm-gone", "prod")).To(Succeed())

				detach := attachNew()
				defer detach()
				Expect(services.SyncAttached(ctx, prevSt, attachedSchema, time.Now())).To(Succeed())
				detach()

				newSt, err := newDB.Store()
				Expect(err).NotTo(HaveOccurred())
				labelsMap, err := newSt.VM().ListLabels(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(labelsMap).To(BeEmpty())
			})
		})

		Context("migration exclusion", func() {
			It("copies migration_excluded=true flags for VMs present in both collections", func() {
				insertSyncTestVM(ctx, prevSt, "vm-1", "alpha")

				newSt, err := newDB.Store()
				Expect(err).NotTo(HaveOccurred())
				insertSyncTestVM(ctx, newSt, "vm-1", "alpha")
				insertSyncTestVM(ctx, newSt, "vm-2", "beta")

				Expect(prevSt.VM().UpdateMigrationExcluded(ctx, "vm-1", true)).To(Succeed())

				detach := attachNew()
				defer detach()
				Expect(services.SyncAttached(ctx, prevSt, attachedSchema, time.Now())).To(Succeed())
				detach()

				newSt, err = newDB.Store()
				Expect(err).NotTo(HaveOccurred())
				states, err := newSt.VM().GetMigrationExcludedStates(ctx, []string{"vm-1", "vm-2"})
				Expect(err).NotTo(HaveOccurred())
				Expect(states["vm-1"]).To(BeTrue())
				Expect(states["vm-2"]).To(BeFalse())
			})
		})

		Context("new VM labeling", func() {
			It("adds LabelNew to VMs absent from the previous collection", func() {
				insertSyncTestVM(ctx, prevSt, "vm-existing", "alpha")

				newSt, err := newDB.Store()
				Expect(err).NotTo(HaveOccurred())
				insertSyncTestVM(ctx, newSt, "vm-existing", "alpha")
				insertSyncTestVM(ctx, newSt, "vm-brand-new", "beta")

				detach := attachNew()
				defer detach()
				Expect(services.SyncAttached(ctx, prevSt, attachedSchema, time.Now())).To(Succeed())
				detach()

				newSt, err = newDB.Store()
				Expect(err).NotTo(HaveOccurred())
				labelsMap, err := newSt.VM().ListLabels(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(labelsMap).NotTo(HaveKey("vm-existing"))
				Expect(labelsMap["vm-brand-new"]).To(ContainElement(services.LabelNew))
			})
		})
	})

	Describe("RefreshGroupInventories", func() {
		It("re-evaluates group filters against new VMs and rebuilds inventory", func() {
			newSt, err := newDB.Store()
			Expect(err).NotTo(HaveOccurred())

			insertSyncTestVM(ctx, newSt, "vm-prod-1", "prod-alpha")
			_, err = newSt.Querier().ExecContext(ctx, `UPDATE vinfo SET "Cluster" = 'prod' WHERE "VM ID" = 'vm-prod-1'`)
			Expect(err).NotTo(HaveOccurred())

			groupID := uuid.New()
			now := time.Now()
			_, err = newSt.Querier().ExecContext(ctx,
				`INSERT INTO groups (id, name, description, filter, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
				groupID, "prod-group", "", "cluster = 'prod'", now, now)
			Expect(err).NotTo(HaveOccurred())

			groupSvc := services.NewGroupService(newSt, &mockInventoryBuilder{})
			_, err = services.RefreshGroupInventories(ctx, newSt, groupSvc)
			Expect(err).To(BeNil())

			vmIDs, err := newSt.Group().GetMatchedIDs(ctx, groupID)
			Expect(err).NotTo(HaveOccurred())
			Expect(vmIDs).To(ContainElement("vm-prod-1"))
		})
	})
})
