package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

var _ = Describe("Source metadata category enrichment", func() {
	It("resolves each category once and preserves unresolved tags and custom attributes", func() {
		ctx := context.Background()
		db, err := sql.Open("duckdb", "")
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = db.Close() }()
		Expect(duckdb_parser.New(db, nil).Init()).To(Succeed())
		_, err = db.Exec(`ALTER TABLE vinfo ADD COLUMN IF NOT EXISTS source_metadata VARCHAR DEFAULT '[]'`)
		Expect(err).NotTo(HaveOccurred())
		metadata := `[{"key":"cat1","value":"Prod","kind":"tag"},{"key":"cat2","value":"QA","kind":"tag"},{"key":"cat1","value":"Finance","kind":"customAttribute"},{"key":"cat1","value":"Payments","kind":"unknown"}]`
		_, err = db.Exec(`INSERT INTO vinfo ("VM ID", "VM", source_metadata) VALUES ('vm1', 'vm1', ?), ('vm2', 'vm2', ?)`, metadata, metadata)
		Expect(err).NotTo(HaveOccurred())
		ids, err := tagCategoryIDs(ctx, db)
		Expect(err).NotTo(HaveOccurred())
		calls := map[string]int{}
		err = resolveTagCategories(ctx, db, ids, func(_ context.Context, id string) (string, error) {
			calls[id]++
			if id == "cat2" {
				return "", errors.New("forbidden")
			}
			return "Environment", nil
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(calls).To(Equal(map[string]int{"cat1": 1, "cat2": 1}))
		rows, err := db.Query(`SELECT source_metadata FROM vinfo`)
		Expect(err).NotTo(HaveOccurred())
		defer func() { _ = rows.Close() }()
		count := 0
		for rows.Next() {
			count++
			var raw string
			Expect(rows.Scan(&raw)).To(Succeed())
			var entries []models.SourceMetadataEntry
			Expect(json.Unmarshal([]byte(raw), &entries)).To(Succeed())
			Expect(entries).To(ConsistOf(
				models.SourceMetadataEntry{Key: "Environment", Value: "Prod", Kind: "tag"},
				models.SourceMetadataEntry{Key: "cat2", Value: "QA", Kind: "tag"},
				models.SourceMetadataEntry{Key: "cat1", Value: "Finance", Kind: "customAttribute"},
				models.SourceMetadataEntry{Key: "cat1", Value: "Payments", Kind: "unknown"},
			))
		}
		Expect(rows.Err()).NotTo(HaveOccurred())
		Expect(count).To(Equal(2))
	})
})
