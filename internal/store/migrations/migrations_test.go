package migrations_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/require"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

func TestMigrations(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Migrations Suite")
}

var _ = Describe("Migrations", func() {
	var (
		ctx    context.Context
		pool   *store.Pool
		mainSt *store.Store
		collSt *store.Store
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "migrations-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)

		mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(tmpDir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
		pool.Add(mainDB)
		mainSt, err = mainDB.Store()
		Expect(err).NotTo(HaveOccurred())

		collDB, err := pool.NewDatabase("coll", filepath.Join(tmpDir, "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(collDB.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
			st, stErr := collDB.Store()
			if stErr != nil {
				return stErr
			}
			if pErr := duckdb_parser.New(st.Querier(), nil).Init(); pErr != nil {
				return pErr
			}
			return migrations.RunCollection(ctx, db, "collection")
		})).To(Succeed())
		pool.Add(collDB)
		collSt, err = collDB.Store()
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

	Context("Main migrations", func() {
		It("should create configuration table", func() {
			_, err := mainSt.Querier().ExecContext(ctx, `
				INSERT INTO configuration (id, agent_mode)
				VALUES (1, 'disconnected')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should add skip_tls and ca_cert columns to credentials table", func() {
			_, err := mainSt.Querier().ExecContext(ctx, `
				INSERT INTO credentials (id, url, username, password, skip_tls, ca_cert)
				VALUES ('test-tls', 'https://vc.local', 'u', 'p', true, 'cert')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should create collections table", func() {
			_, err := mainSt.Querier().ExecContext(ctx, `
				INSERT INTO collections ("database", state)
				VALUES ('collection_default', 'running')
			`)
			Expect(err).NotTo(HaveOccurred())

			var ns string
			err = mainSt.Querier().QueryRowContext(ctx, `SELECT "database" FROM collections WHERE "database" = 'collection_default'`).Scan(&ns)
			Expect(err).NotTo(HaveOccurred())
			Expect(ns).To(Equal("collection_default"))
		})

		It("should track applied main migrations", func() {
			rows, err := mainSt.Querier().QueryContext(ctx, `SELECT version FROM schema_migrations ORDER BY version`)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = rows.Close() }()

			var versions []int
			for rows.Next() {
				var v int
				Expect(rows.Scan(&v)).To(Succeed())
				versions = append(versions, v)
			}
			Expect(rows.Err()).NotTo(HaveOccurred())
			Expect(versions).To(ContainElement(1))

			for i, v := range versions {
				Expect(v).To(Equal(i + 1))
			}
		})
	})

	Context("Collection migrations", func() {
		It("preserves tag categories when the collection is cloned", func() {
			_, err := collSt.Querier().ExecContext(ctx, `INSERT INTO tag_categories VALUES ('cat1', 'Owner')`)
			Expect(err).NotTo(HaveOccurred())
			db, err := pool.Get("coll")
			Expect(err).NotTo(HaveOccurred())
			clone, err := db.Clone(ctx)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(clone.Close)
			st, err := clone.Store()
			Expect(err).NotTo(HaveOccurred())
			var name string
			Expect(st.Querier().QueryRowContext(ctx, `SELECT name FROM tag_categories WHERE id = 'cat1'`).Scan(&name)).To(Succeed())
			Expect(name).To(Equal("Owner"))
		})

		It("should create inventory table", func() {
			_, err := collSt.Querier().ExecContext(ctx, `
				INSERT INTO inventory (id, data)
				VALUES (1, 'test data')
			`)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should track applied collection migrations", func() {
			rows, err := collSt.Querier().QueryContext(ctx, `SELECT version FROM collection_schema_migrations ORDER BY version`)
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = rows.Close() }()

			var versions []int
			for rows.Next() {
				var v int
				Expect(rows.Scan(&v)).To(Succeed())
				versions = append(versions, v)
			}
			Expect(rows.Err()).NotTo(HaveOccurred())
			Expect(versions).To(ContainElement(1))
		})
	})

	Context("Idempotency", func() {
		It("should be safe to run main migrations twice", func() {
			mainDB, err := pool.Get(store.MainDatabaseID)
			Expect(err).NotTo(HaveOccurred())
			Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
		})

		It("should be safe to run collection migrations twice", func() {
			collDB, err := pool.Get("coll")
			Expect(err).NotTo(HaveOccurred())
			Expect(collDB.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
				return migrations.RunCollection(ctx, db, "collection")
			})).To(Succeed())
		})
	})
})

func TestRDMLunUUIDCollectionMigration(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, duckdb_parser.New(db, nil).Init())
	require.NoError(t, migrations.RunCollection(ctx, db, "memory"))
	_, err = db.Exec(`DELETE FROM collection_schema_migrations WHERE version=2;
 ALTER TABLE vdisk DROP COLUMN "RDM LUN UUID";
 INSERT INTO vinfo ("VM ID","VM") VALUES ('vm-1','legacy-vm');
 INSERT INTO vdisk ("VM ID","Raw","Capacity MiB","Disk UUID") VALUES ('vm-1',true,100,'not-a-lun-uuid');`)
	require.NoError(t, err)
	require.NoError(t, migrations.RunCollection(ctx, db, "memory"))
	require.NoError(t, migrations.RunCollection(ctx, db, "memory"))
	var capacity int64
	var uuid sql.NullString
	require.NoError(t, db.QueryRow(`SELECT "Capacity MiB","RDM LUN UUID" FROM vdisk`).Scan(&capacity, &uuid))
	require.Equal(t, int64(100), capacity)
	require.False(t, uuid.Valid)
	_, err = db.Exec(`UPDATE vdisk SET "RDM LUN UUID"='collected-lun-uuid'; DELETE FROM collection_schema_migrations WHERE version=2;`)
	require.NoError(t, err)
	require.NoError(t, migrations.RunCollection(ctx, db, "memory"))
	require.NoError(t, db.QueryRow(`SELECT "RDM LUN UUID" FROM vdisk`).Scan(&uuid))
	require.Equal(t, "collected-lun-uuid", uuid.String, "idempotent column addition must preserve supplied UUIDs")
}

func TestPreStorageCollectionMigration(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	schema, err := os.ReadFile("../testdata/pre_storage_schema.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(schema))
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO vdatastore ("Object ID", "Name", "Type", "Capacity MiB") VALUES ('ds-1', 'legacy-ds', 'VMFS', 2048);
INSERT INTO vhba ("Device", "Type") VALUES ('vmhba64', 'ISCSI');`)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, migrations.RunCollection(ctx, db, "memory"))
	require.NoError(t, migrations.RunCollection(ctx, db, "memory"))
	var appliedVersions string
	require.NoError(t, db.QueryRow(`SELECT string_agg(version::VARCHAR, ',' ORDER BY version) FROM collection_schema_migrations`).Scan(&appliedVersions))
	require.Equal(t, "1,2", appliedVersions)
	var fsType, protocol, hbaType sql.NullString
	var version sql.NullInt32
	var capacity float64
	require.NoError(t, db.QueryRow(`SELECT "Filesystem Type", "Filesystem Major Version", "Storage Protocol", "Capacity MiB" FROM vdatastore`).Scan(&fsType, &version, &protocol, &capacity))
	require.False(t, fsType.Valid)
	require.False(t, version.Valid)
	require.False(t, protocol.Valid)
	require.Equal(t, float64(2048), capacity)
	require.NoError(t, db.QueryRow(`SELECT "Type" FROM vhba WHERE "Device"='vmhba64'`).Scan(&hbaType))
	require.Equal(t, "ISCSI", hbaType.String)
	_, err = db.Exec(`UPDATE vdatastore SET "Filesystem Type"='VMFS', "Filesystem Major Version"=6, "Storage Protocol"='iSCSI';
DELETE FROM collection_schema_migrations WHERE version=2;`)
	require.NoError(t, err)
	require.NoError(t, migrations.RunCollection(ctx, db, "memory"))
	require.NoError(t, db.QueryRow(`SELECT "Filesystem Type", "Filesystem Major Version", "Storage Protocol" FROM vdatastore`).Scan(&fsType, &version, &protocol))
	require.Equal(t, "VMFS", fsType.String)
	require.Equal(t, int32(6), version.Int32)
	require.Equal(t, "iSCSI", protocol.String)
}
