package store_test

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

var _ = Describe("Pool", func() {
	var (
		ctx    context.Context
		pool   *store.Pool
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "pool-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)
	})

	AfterEach(func() {
		if pool != nil {
			pool.Close()
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	Context("NewDatabase", func() {
		It("should register a database and return a stable ID", func() {
			dbPath := filepath.Join(tmpDir, "test.duckdb")
			db1, err := pool.NewDatabase("test", dbPath, time.Now(), store.LazyConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			Expect(db1.ID).NotTo(BeEmpty())

			db2, err := pool.NewDatabase("test", dbPath, time.Now(), store.LazyConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			Expect(db1.ID).To(Equal(db2.ID))
		})

		It("should eagerly open a usable connection", func() {
			dbPath := filepath.Join(tmpDir, "eager.duckdb")
			db, err := pool.NewDatabase("test", dbPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())

			st, err := db.Store()
			Expect(err).NotTo(HaveOccurred())
			Expect(st.VerifyConnection(ctx)).To(Succeed())
		})
	})

	Context("Store", func() {
		It("should lazily connect and execute queries", func() {
			dbPath := filepath.Join(tmpDir, "lazy.duckdb")
			db, err := pool.NewDatabase("test", dbPath, time.Now(), store.LazyConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			pool.Add(db)

			st, err := db.Store()
			Expect(err).NotTo(HaveOccurred())
			Expect(st.VerifyConnection(ctx)).To(Succeed())
		})

		It("should return the same store instance for the same database", func() {
			dbPath := filepath.Join(tmpDir, "same.duckdb")
			db, err := pool.NewDatabase("test", dbPath, time.Now(), store.LazyConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			pool.Add(db)

			st1, err := db.Store()
			Expect(err).NotTo(HaveOccurred())

			st2, err := db.Store()
			Expect(err).NotTo(HaveOccurred())

			Expect(st1).To(BeIdenticalTo(st2))
		})

		It("should return error for unknown ID", func() {
			_, err := pool.Get("nonexistent")
			Expect(err).To(HaveOccurred())
		})

		It("should not close a recently queried DB", func() {
			pool.Close()
			pool = store.NewPool(1 * time.Second)

			dbPath := filepath.Join(tmpDir, "recent.duckdb")
			db, err := pool.NewDatabase("test", dbPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			pool.Add(db)

			st, err := db.Store()
			Expect(err).NotTo(HaveOccurred())
			Expect(st.VerifyConnection(ctx)).To(Succeed())

			time.Sleep(5 * time.Millisecond)
			st2, err := db.Store()
			Expect(err).NotTo(HaveOccurred())
			Expect(st2.VerifyConnection(ctx)).To(Succeed())
			Expect(st).To(BeIdenticalTo(st2))
		})

		It("should close a DB idle longer than the interval", func() {
			pool.Close()
			pool = store.NewPool(1 * time.Millisecond)

			dbPath := filepath.Join(tmpDir, "idle.duckdb")
			db, err := pool.NewDatabase("test", dbPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			pool.Add(db)

			st, err := db.Store()
			Expect(err).NotTo(HaveOccurred())
			Expect(st.VerifyConnection(ctx)).To(Succeed())

			time.Sleep(10 * time.Millisecond)

			// Trigger cleanup via Get, then reconnect.
			db2, err := pool.Get(db.ID)
			Expect(err).NotTo(HaveOccurred())
			st2, err := db2.Store()
			Expect(err).NotTo(HaveOccurred())
			Expect(st2.VerifyConnection(ctx)).To(Succeed())
		})

		It("should close long-idle DB but keep short-idle DB in same cleanup pass", func() {
			pool.Close()
			pool = store.NewPool(50 * time.Millisecond)

			longIdlePath := filepath.Join(tmpDir, "long.duckdb")
			longDB, err := pool.NewDatabase("long", longIdlePath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			pool.Add(longDB)

			stLong, err := longDB.Store()
			Expect(err).NotTo(HaveOccurred())
			Expect(stLong.VerifyConnection(ctx)).To(Succeed())

			time.Sleep(60 * time.Millisecond)

			shortIdlePath := filepath.Join(tmpDir, "short.duckdb")
			shortDB, err := pool.NewDatabase("short", shortIdlePath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			pool.Add(shortDB)

			stShort, err := shortDB.Store()
			Expect(err).NotTo(HaveOccurred())
			Expect(stShort.VerifyConnection(ctx)).To(Succeed())

			// Trigger cleanup via Get. Long-idle should be closed, short-idle should survive.
			shortDB2, err := pool.Get(shortDB.ID)
			Expect(err).NotTo(HaveOccurred())
			stShort2, err := shortDB2.Store()
			Expect(err).NotTo(HaveOccurred())
			Expect(stShort).To(BeIdenticalTo(stShort2))

			longDB2, err := pool.Get(longDB.ID)
			Expect(err).NotTo(HaveOccurred())
			stLong2, err := longDB2.Store()
			Expect(err).NotTo(HaveOccurred())
			Expect(stLong2.VerifyConnection(ctx)).To(Succeed())
			Expect(stLong).NotTo(BeIdenticalTo(stLong2))
		})
	})

	Context("List", func() {
		It("should return all registered databases", func() {
			dbA, err := pool.NewDatabase("a", filepath.Join(tmpDir, "a.duckdb"), time.Now(), store.LazyConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			pool.Add(dbA)
			dbB, err := pool.NewDatabase("b", filepath.Join(tmpDir, "b.duckdb"), time.Now(), store.LazyConnectionInitilization, 0, store.ReadOnlyDatabase)
			Expect(err).NotTo(HaveOccurred())
			pool.Add(dbB)

			Expect(pool.List()).To(HaveLen(2))
		})

		It("should return empty when no databases registered", func() {
			Expect(pool.List()).To(BeEmpty())
		})
	})

	Context("AttachDatabase / DetachDatabase", func() {
		It("should attach b to a as readonly after closing b", func() {
			dbA, err := pool.NewDatabase("a", filepath.Join(tmpDir, "a.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			pool.Add(dbA)

			dbB, err := pool.NewDatabase("b", filepath.Join(tmpDir, "b.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			pool.Add(dbB)

			// Close B otherwise error occurs
			stB, err := dbB.Store()
			Expect(err).NotTo(HaveOccurred())

			_, err = stB.Querier().ExecContext(ctx, "CREATE TABLE hello (id INTEGER)")
			Expect(err).NotTo(HaveOccurred())
			_, err = stB.Querier().ExecContext(ctx, "INSERT INTO hello VALUES (42)")
			Expect(err).NotTo(HaveOccurred())

			Expect(dbB.Close()).To(Succeed())

			stA, err := dbA.Store()
			Expect(err).NotTo(HaveOccurred())

			Expect(stA.AttachDatabase(ctx, dbB, "b", store.ReadOnlyDatabase)).To(Succeed())

			var val int
			err = stA.Querier().QueryRowContext(ctx, "SELECT id FROM b.hello").Scan(&val)
			Expect(err).NotTo(HaveOccurred())
			Expect(val).To(Equal(42))

			Expect(stA.DetachDatabase(ctx, "b")).To(Succeed())
		})
	})

	Context("Append", func() {
		It("copies rows from shared main tables by column name", func() {
			destination, err := pool.NewDatabase("destination", filepath.Join(tmpDir, "destination.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			source, err := pool.NewDatabase("source", filepath.Join(tmpDir, "source.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())

			destinationStore, err := destination.Store()
			Expect(err).NotTo(HaveOccurred())
			sourceStore, err := source.Store()
			Expect(err).NotTo(HaveOccurred())

			_, err = destinationStore.Querier().ExecContext(ctx, `CREATE TABLE first (id INTEGER PRIMARY KEY, name VARCHAR)`)
			Expect(err).NotTo(HaveOccurred())
			_, err = destinationStore.Querier().ExecContext(ctx, `CREATE TABLE "second table" (id INTEGER PRIMARY KEY, value VARCHAR)`)
			Expect(err).NotTo(HaveOccurred())
			_, err = destinationStore.Querier().ExecContext(ctx, `CREATE TABLE about (product VARCHAR)`)
			Expect(err).NotTo(HaveOccurred())
			_, err = sourceStore.Querier().ExecContext(ctx, `CREATE TABLE first (name VARCHAR, id INTEGER PRIMARY KEY)`)
			Expect(err).NotTo(HaveOccurred())
			_, err = sourceStore.Querier().ExecContext(ctx, `CREATE TABLE "second table" (value VARCHAR, id INTEGER PRIMARY KEY)`)
			Expect(err).NotTo(HaveOccurred())
			_, err = sourceStore.Querier().ExecContext(ctx, `CREATE TABLE about (product VARCHAR)`)
			Expect(err).NotTo(HaveOccurred())
			_, err = sourceStore.Querier().ExecContext(ctx, `INSERT INTO first VALUES ('source', 7)`)
			Expect(err).NotTo(HaveOccurred())
			_, err = sourceStore.Querier().ExecContext(ctx, `INSERT INTO "second table" VALUES ('copied', 8)`)
			Expect(err).NotTo(HaveOccurred())
			_, err = sourceStore.Querier().ExecContext(ctx, `INSERT INTO about VALUES ('vCenter')`)
			Expect(err).NotTo(HaveOccurred())
			Expect(source.Close()).To(Succeed())

			Expect(destination.Append(ctx, source)).To(Succeed())

			var id int
			var value string
			Expect(destinationStore.Querier().QueryRowContext(ctx, `SELECT id, name FROM first`).Scan(&id, &value)).To(Succeed())
			Expect(id).To(Equal(7))
			Expect(value).To(Equal("source"))
			Expect(destinationStore.Querier().QueryRowContext(ctx, `SELECT id, value FROM "second table"`).Scan(&id, &value)).To(Succeed())
			Expect(id).To(Equal(8))
			Expect(value).To(Equal("copied"))
			Expect(destinationStore.Querier().QueryRowContext(ctx, `SELECT product FROM about`).Scan(&value)).To(Succeed())
			Expect(value).To(Equal("vCenter"))
		})

		It("skips source tables missing from the destination", func() {
			destination, err := pool.NewDatabase("destination", filepath.Join(tmpDir, "destination.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			source, err := pool.NewDatabase("source", filepath.Join(tmpDir, "source.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			sourceStore, err := source.Store()
			Expect(err).NotTo(HaveOccurred())
			_, err = sourceStore.Querier().ExecContext(ctx, `CREATE TABLE source_only (id INTEGER)`)
			Expect(err).NotTo(HaveOccurred())
			Expect(source.Close()).To(Succeed())

			Expect(destination.Append(ctx, source)).To(Succeed())

			destinationStore, err := destination.Store()
			Expect(err).NotTo(HaveOccurred())
			var count int
			Expect(destinationStore.Querier().QueryRowContext(ctx,
				`SELECT count(*) FROM duckdb_tables() WHERE database_name = current_database() AND table_name = 'source_only'`,
			).Scan(&count)).To(Succeed())
			Expect(count).To(BeZero())
		})

		It("skips conflicting rows and copies the remaining rows", func() {
			destination, err := pool.NewDatabase("destination", filepath.Join(tmpDir, "destination.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			source, err := pool.NewDatabase("source", filepath.Join(tmpDir, "source.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())

			destinationStore, err := destination.Store()
			Expect(err).NotTo(HaveOccurred())
			sourceStore, err := source.Store()
			Expect(err).NotTo(HaveOccurred())
			for _, st := range []*store.Store{destinationStore, sourceStore} {
				_, err = st.Querier().ExecContext(ctx, `CREATE TABLE first (id INTEGER PRIMARY KEY)`)
				Expect(err).NotTo(HaveOccurred())
				_, err = st.Querier().ExecContext(ctx, `CREATE TABLE second (id INTEGER PRIMARY KEY, value VARCHAR)`)
				Expect(err).NotTo(HaveOccurred())
			}
			_, err = sourceStore.Querier().ExecContext(ctx, `INSERT INTO first VALUES (1)`)
			Expect(err).NotTo(HaveOccurred())
			_, err = sourceStore.Querier().ExecContext(ctx, `INSERT INTO second VALUES (2, 'source')`)
			Expect(err).NotTo(HaveOccurred())
			_, err = sourceStore.Querier().ExecContext(ctx, `INSERT INTO second VALUES (3, 'new')`)
			Expect(err).NotTo(HaveOccurred())
			_, err = destinationStore.Querier().ExecContext(ctx, `INSERT INTO second VALUES (2, 'existing')`)
			Expect(err).NotTo(HaveOccurred())
			Expect(source.Close()).To(Succeed())

			Expect(destination.Append(ctx, source)).To(Succeed())

			var count int
			Expect(destinationStore.Querier().QueryRowContext(ctx, `SELECT count(*) FROM first WHERE id = 1`).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(1))
			Expect(destinationStore.Querier().QueryRowContext(ctx, `SELECT count(*) FROM second`).Scan(&count)).To(Succeed())
			Expect(count).To(Equal(2))
			var value string
			Expect(destinationStore.Querier().QueryRowContext(ctx, `SELECT value FROM second WHERE id = 2`).Scan(&value)).To(Succeed())
			Expect(value).To(Equal("existing"))
			Expect(destinationStore.Querier().QueryRowContext(ctx, `SELECT value FROM second WHERE id = 3`).Scan(&value)).To(Succeed())
			Expect(value).To(Equal("new"))
		})
	})

	Context("LatestCollection", func() {
		It("returns false when pool has only the main database", func() {
			mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(tmpDir, "main.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			pool.Add(mainDB)

			db, err := pool.Latest()
			Expect(err).NotTo(BeNil())
			Expect(db).To(BeNil())
		})

		It("returns the newest collection database, skipping main", func() {
			mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(tmpDir, "main.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			pool.Add(mainDB)

			older, err := pool.NewDatabase("col-old", filepath.Join(tmpDir, "col-old.duckdb"), time.Now().Add(-1*time.Hour), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			pool.Add(older)

			newer, err := pool.NewDatabase("col-new", filepath.Join(tmpDir, "col-new.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			pool.Add(newer)

			db, err := pool.Latest()
			Expect(err).To(BeNil())
			Expect(db.ID).To(Equal("col-new"))
		})
	})

	Context("Close", func() {
		It("should close all connections without removing entries", func() {
			dbPath := filepath.Join(tmpDir, "close.duckdb")
			db, err := pool.NewDatabase("test", dbPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			pool.Add(db)

			pool.Close()
			Expect(pool.List()).To(HaveLen(1))
		})
	})
})
