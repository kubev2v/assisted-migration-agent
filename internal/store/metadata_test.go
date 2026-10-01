package store_test

import (
	"context"
	"database/sql"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

var _ = Describe("MetadataStore", func() {
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
