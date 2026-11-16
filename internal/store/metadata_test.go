package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

var _ = Describe("MetadataStore", func() {
	Context("used tag category IDs", func() {
		var (
			ctx        context.Context
			db         *sql.DB
			sqlitePath string
		)

		BeforeEach(func() {
			ctx = context.Background()
			var err error
			db, err = sql.Open("duckdb", "")
			Expect(err).NotTo(HaveOccurred())
			db.SetMaxOpenConns(1)
			DeferCleanup(db.Close)
			Expect(duckdb_parser.New(db, nil).Init()).To(Succeed())

			sqlitePath = filepath.Join(GinkgoT().TempDir(), "collector's inventory.db")
			escapedPath := strings.ReplaceAll(sqlitePath, "'", "''")
			_, err = db.ExecContext(ctx, fmt.Sprintf("ATTACH '%s' AS inventory_fixture (TYPE sqlite)", escapedPath))
			Expect(err).NotTo(HaveOccurred())
		})

		It("deduplicates IDs and ignores templates and unusable tags", func() {
			_, err := db.ExecContext(ctx, "CREATE TABLE inventory_fixture.VM (IsTemplate INTEGER, Tags VARCHAR)")
			Expect(err).NotTo(HaveOccurred())

			tags := []map[string]string{
				{"categoryID": "cat-1", "name": "production"},
				{"categoryID": "cat-1", "name": "staging"},
				{"categoryID": "cat-'2", "name": "database"},
				{"categoryID": "", "name": "missing category"},
				{"categoryID": "  ", "name": "blank category"},
				{"name": "absent category"},
				{"categoryID": "empty-name", "name": ""},
				{"categoryID": "blank-name", "name": "  "},
			}
			tagsJSON, err := json.Marshal(tags)
			Expect(err).NotTo(HaveOccurred())
			_, err = db.ExecContext(ctx, `
				INSERT INTO inventory_fixture.VM VALUES
					(0, ?), (0, ?), (0, NULL), (0, '[]'), (0, ''), (0, '   '), (1, ?)
			`,
				string(tagsJSON), string(tagsJSON), `[{"categoryID":"template-only","name":"template"}]`)
			Expect(err).NotTo(HaveOccurred())
			_, err = db.ExecContext(ctx, "DETACH inventory_fixture")
			Expect(err).NotTo(HaveOccurred())

			ids, err := store.NewMetadataStore(db).ListUsedTagCategoryIDs(ctx, sqlitePath)
			Expect(err).NotTo(HaveOccurred())
			Expect(ids).To(ConsistOf("cat-1", "cat-'2"))
		})

		It("filters templates when SQLite declares IsTemplate as BOOLEAN", func() {
			_, err := db.ExecContext(ctx, "CREATE TABLE inventory_fixture.VM (IsTemplate BOOLEAN, Tags VARCHAR)")
			Expect(err).NotTo(HaveOccurred())
			_, err = db.ExecContext(ctx, "INSERT INTO inventory_fixture.VM VALUES (false, ?), (true, ?)",
				`[{"categoryID":"vm-category","name":"production"}]`,
				`[{"categoryID":"template-only","name":"template"}]`)
			Expect(err).NotTo(HaveOccurred())
			_, err = db.ExecContext(ctx, "DETACH inventory_fixture")
			Expect(err).NotTo(HaveOccurred())

			ids, err := store.NewMetadataStore(db).ListUsedTagCategoryIDs(ctx, sqlitePath)
			Expect(err).NotTo(HaveOccurred())
			Expect(ids).To(ConsistOf("vm-category"))
		})

		It("reports inventory missing the required Tags column", func() {
			_, err := db.ExecContext(ctx, "CREATE TABLE inventory_fixture.VM (IsTemplate INTEGER)")
			Expect(err).NotTo(HaveOccurred())
			_, err = db.ExecContext(ctx, "INSERT INTO inventory_fixture.VM VALUES (0)")
			Expect(err).NotTo(HaveOccurred())
			_, err = db.ExecContext(ctx, "DETACH inventory_fixture")
			Expect(err).NotTo(HaveOccurred())

			_, err = store.NewMetadataStore(db).ListUsedTagCategoryIDs(ctx, sqlitePath)
			Expect(err).To(MatchError(ContainSubstring("reading used tag category IDs")))
		})

		It("reports inventory query failures", func() {
			_, err := db.ExecContext(ctx, "DETACH inventory_fixture")
			Expect(err).NotTo(HaveOccurred())

			_, err = store.NewMetadataStore(db).ListUsedTagCategoryIDs(ctx, sqlitePath)
			Expect(err).To(MatchError(ContainSubstring("reading used tag category IDs")))
		})
	})

	It("saves quoted names in the migrated table and accepts empty input", func() {
		db, err := sql.Open("duckdb", "")
		Expect(err).NotTo(HaveOccurred())
		db.SetMaxOpenConns(1)
		DeferCleanup(db.Close)
		ctx := context.Background()
		Expect(duckdb_parser.New(db, nil).Init()).To(Succeed())
		Expect(migrations.RunCollection(ctx, db, "memory")).To(Succeed())
		Expect(store.NewMetadataStore(db).SaveTagCategories(ctx, nil)).To(Succeed())
		var count int
		Expect(db.QueryRowContext(ctx, `SELECT count(*) FROM tag_categories`).Scan(&count)).To(Succeed())
		Expect(count).To(BeZero())
		Expect(store.NewMetadataStore(db).SaveTagCategories(ctx, map[string]string{"cat1": "Owner's; \"name\"", "cat2": "Environment"})).To(Succeed())
		var name string
		Expect(db.QueryRowContext(ctx, `SELECT name FROM tag_categories WHERE id = 'cat1'`).Scan(&name)).To(Succeed())
		Expect(name).To(Equal("Owner's; \"name\""))
		Expect(db.QueryRowContext(ctx, `SELECT count(*) FROM tag_categories`).Scan(&count)).To(Succeed())
		Expect(count).To(Equal(2))
	})
})
