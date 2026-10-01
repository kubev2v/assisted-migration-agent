package services

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
)

var _ = Describe("Metadata category names", func() {
	DescribeTable("cleans up staging before collection cloning",
		func(cancelled bool) {
			dir, err := os.MkdirTemp("", "metadata-clone-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(os.RemoveAll, dir)
			pool := store.NewPool(time.Minute)
			DeferCleanup(pool.Close)
			db, err := pool.NewDatabase("metadata", filepath.Join(dir, "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			st, err := db.Store()
			Expect(err).NotTo(HaveOccurred())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err = st.Querier().ExecContext(ctx, `CREATE TABLE tag_categories (id VARCHAR); INSERT INTO tag_categories VALUES ('permanent')`)
			Expect(err).NotTo(HaveOccurred())
			Expect(saveTagCategoryNames(ctx, st.Querier(), map[string]string{"cat1": "owner"})).To(Succeed())
			if cancelled {
				cancel()
			}
			Expect(dropTagCategoryNames(ctx, st.Querier())).To(Succeed())
			Expect(dropTagCategoryNames(ctx, st.Querier())).To(Succeed())
			var temporaryTables int
			Expect(st.Querier().QueryRowContext(context.Background(), `SELECT count(*) FROM information_schema.tables WHERE table_catalog = 'temp' AND table_name = 'tag_categories'`).Scan(&temporaryTables)).To(Succeed())
			Expect(temporaryTables).To(BeZero())
			clone, err := db.Clone(context.Background())
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(clone.Close)
			cloneStore, err := clone.Store()
			Expect(err).NotTo(HaveOccurred())
			var id string
			Expect(cloneStore.Querier().QueryRowContext(context.Background(), `SELECT id FROM tag_categories`).Scan(&id)).To(Succeed())
			Expect(id).To(Equal("permanent"))
		},
		Entry("with an active collection context", false),
		Entry("with a cancelled collection context", true),
	)

	It("stages quoted names for ingestion and replaces stale mappings with an empty table", func() {
		db, err := sql.Open("duckdb", "")
		Expect(err).NotTo(HaveOccurred())
		db.SetMaxOpenConns(1)
		DeferCleanup(db.Close)
		ctx := context.Background()
		Expect(saveTagCategoryNames(ctx, db, map[string]string{"cat1": "Owner's; \"name\""})).To(Succeed())
		var name string
		Expect(db.QueryRowContext(ctx, `SELECT name FROM temp.main.tag_categories WHERE id = 'cat1'`).Scan(&name)).To(Succeed())
		Expect(name).To(Equal("Owner's; \"name\""))
		Expect(saveTagCategoryNames(ctx, db, nil)).To(Succeed())
		var count int
		Expect(db.QueryRowContext(ctx, `SELECT count(*) FROM temp.main.tag_categories`).Scan(&count)).To(Succeed())
		Expect(count).To(BeZero())
	})

	It("retains successful names when another category cannot be resolved", func() {
		calls := map[string]int{}
		names := resolveTagCategories(context.Background(), []string{"cat1", "cat2", "cat3"}, func(_ context.Context, id string) (string, error) {
			calls[id]++
			switch id {
			case "cat1":
				return "Owner's; \"name\"", nil
			case "cat2":
				return "", errors.New("forbidden")
			default:
				return "", nil
			}
		})
		Expect(names).To(Equal(map[string]string{"cat1": "Owner's; \"name\""}))
		Expect(calls).To(Equal(map[string]int{"cat1": 1, "cat2": 1, "cat3": 1}))
		Expect(tagCategoryNames(context.Background(), nil, models.Credentials{})).To(BeNil())
	})

	It("stops lookups on cancellation and retains names already resolved", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		calls := 0
		names := resolveTagCategories(ctx, []string{"cat1", "cat2"}, func(context.Context, string) (string, error) {
			calls++
			cancel()
			return "Owner", nil
		})
		Expect(names).To(Equal(map[string]string{"cat1": "Owner"}))
		Expect(calls).To(Equal(1))
	})
})
