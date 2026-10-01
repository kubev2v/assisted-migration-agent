package store_test

import (
	"context"
	"database/sql"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

var _ = Describe("MetadataStore", func() {
	It("stages quoted names for ingestion and replaces stale mappings with an empty table", func() {
		db, err := sql.Open("duckdb", "")
		Expect(err).NotTo(HaveOccurred())
		db.SetMaxOpenConns(1)
		DeferCleanup(db.Close)
		ctx := context.Background()
		Expect(store.NewMetadataStore(db).SaveTagCategories(ctx, map[string]string{"cat1": "Owner's; \"name\""})).To(Succeed())
		var name string
		Expect(db.QueryRowContext(ctx, `SELECT name FROM temp.main.tag_categories WHERE id = 'cat1'`).Scan(&name)).To(Succeed())
		Expect(name).To(Equal("Owner's; \"name\""))
		Expect(store.NewMetadataStore(db).SaveTagCategories(ctx, nil)).To(Succeed())
		var count int
		Expect(db.QueryRowContext(ctx, `SELECT count(*) FROM temp.main.tag_categories`).Scan(&count)).To(Succeed())
		Expect(count).To(BeZero())
	})
})
