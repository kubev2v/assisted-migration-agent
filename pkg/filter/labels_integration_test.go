package filter

import (
	"database/sql"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/duckdb/duckdb-go/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Labels Filter Integration with DuckDB", func() {
	var db *sql.DB

	// labelsMapper maps filter variable names to SQL column references.
	var labelsMapper MapFunc = func(name string) (string, FieldType, error) {
		switch name {
		case "name":
			return `"name"`, StringField, nil
		case "cluster":
			return `"cluster"`, StringField, nil
		case "labels":
			return `"labels"`, ArrayField, nil
		default:
			return "", 0, fmt.Errorf("unknown field: %s", name)
		}
	}

	BeforeEach(func() {
		var err error
		connector, err := duckdb.NewConnector("", nil)
		Expect(err).ToNot(HaveOccurred())

		db = sql.OpenDB(connector)
		Expect(db.Ping()).To(Succeed())

		// Create a VM table with labels array column
		_, err = db.Exec(`CREATE TABLE vms (
			"name"    VARCHAR NOT NULL,
			"cluster" VARCHAR NOT NULL,
			"labels"  VARCHAR DEFAULT '[]'
		)`)
		Expect(err).ToNot(HaveOccurred())

		// Insert test data with various label combinations
		_, err = db.Exec(`INSERT INTO vms VALUES
			('vm-web-01',      'prod-east',    ['production', 'critical', 'wave-1']),
			('vm-web-02',      'prod-west',    ['production', 'wave-2']),
			('vm-db-01',       'prod-east',    ['production', 'critical', 'database']),
			('vm-db-02',       'prod-west',    ['production', 'database', 'wave-1']),
			('vm-cache-01',    'prod-east',    ['production', 'cache']),
			('vm-worker-01',   'staging',      ['staging', 'worker']),
			('vm-worker-02',   'staging',      ['staging', 'worker', 'critical']),
			('vm-test-01',     'test',         ['test', 'temporary']),
			('vm-test-02',     'test',         ['test']),
			('vm-legacy',      'prod-east',    [])
		`)
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	queryVMs := func(filterExpr string) ([]string, error) {
		expr, err := parse([]byte(filterExpr))
		if err != nil {
			return nil, err
		}

		sqlizer, err := toSql(expr, labelsMapper)
		if err != nil {
			return nil, err
		}

		query, args, err := sq.Select(`"name"`).From("vms").Where(sqlizer).OrderBy(`"name"`).ToSql()
		if err != nil {
			return nil, fmt.Errorf("query build failed: %w", err)
		}

		rows, err := db.Query(query, args...)
		if err != nil {
			return nil, fmt.Errorf("query failed: %w\nQuery: %s\nArgs: %v", err, query, args)
		}
		defer func() { _ = rows.Close() }()

		var names []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return nil, err
			}
			names = append(names, name)
		}
		return names, rows.Err()
	}

	Context("CONTAINS operator", func() {
		It("should find VMs with 'production' label", func() {
			names, err := queryVMs("labels contains 'production'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-cache-01", "vm-db-01", "vm-db-02", "vm-web-01", "vm-web-02"}))
		})

		It("should find VMs with 'critical' label", func() {
			names, err := queryVMs("labels contains 'critical'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-web-01", "vm-worker-02"}))
		})

		It("should find VMs with 'database' label", func() {
			names, err := queryVMs("labels contains 'database'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-db-02"}))
		})

		It("should find VMs with 'staging' label", func() {
			names, err := queryVMs("labels contains 'staging'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-worker-01", "vm-worker-02"}))
		})

		It("should find VMs with 'test' label", func() {
			names, err := queryVMs("labels contains 'test'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-test-01", "vm-test-02"}))
		})

		It("should return empty for non-existent label", func() {
			names, err := queryVMs("labels contains 'nonexistent'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})

		It("should find VMs with 'wave-1' label", func() {
			names, err := queryVMs("labels contains 'wave-1'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-02", "vm-web-01"}))
		})
	})

	Context("NOT CONTAINS operator", func() {
		It("should find VMs without 'production' label", func() {
			names, err := queryVMs("labels not contains 'production'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-legacy", "vm-test-01", "vm-test-02", "vm-worker-01", "vm-worker-02"}))
		})

		It("should find VMs without 'critical' label", func() {
			names, err := queryVMs("labels not contains 'critical'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-cache-01", "vm-db-02", "vm-legacy", "vm-test-01", "vm-test-02", "vm-web-02", "vm-worker-01"}))
		})

		It("should find VMs without 'database' label", func() {
			names, err := queryVMs("labels not contains 'database'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-cache-01", "vm-legacy", "vm-test-01", "vm-test-02", "vm-web-01", "vm-web-02", "vm-worker-01", "vm-worker-02"}))
		})

		It("should find all VMs when excluding non-existent label", func() {
			names, err := queryVMs("labels not contains 'nonexistent'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(10))
		})
	})

	Context("CONTAINS with AND", func() {
		It("should find VMs with both 'production' and 'critical' labels", func() {
			names, err := queryVMs("labels contains 'production' and labels contains 'critical'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-web-01"}))
		})

		It("should find VMs with both 'production' and 'database' labels", func() {
			names, err := queryVMs("labels contains 'production' and labels contains 'database'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-db-02"}))
		})

		It("should find VMs with both 'staging' and 'worker' labels", func() {
			names, err := queryVMs("labels contains 'staging' and labels contains 'worker'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-worker-01", "vm-worker-02"}))
		})

		It("should return empty when combining labels that don't coexist", func() {
			names, err := queryVMs("labels contains 'production' and labels contains 'test'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})

		It("should combine labels with string field", func() {
			names, err := queryVMs("cluster = 'prod-east' and labels contains 'critical'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-web-01"}))
		})

		It("should combine string field with labels", func() {
			names, err := queryVMs("labels contains 'database' and cluster = 'prod-west'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-02"}))
		})
	})

	Context("CONTAINS with OR", func() {
		It("should find VMs with either 'production' or 'staging' label", func() {
			names, err := queryVMs("labels contains 'production' or labels contains 'staging'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-cache-01", "vm-db-01", "vm-db-02", "vm-web-01", "vm-web-02", "vm-worker-01", "vm-worker-02"}))
		})

		It("should find VMs with either 'database' or 'cache' label", func() {
			names, err := queryVMs("labels contains 'database' or labels contains 'cache'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-cache-01", "vm-db-01", "vm-db-02"}))
		})

		It("should combine labels OR with string field", func() {
			names, err := queryVMs("labels contains 'critical' or cluster = 'test'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-test-01", "vm-test-02", "vm-web-01", "vm-worker-02"}))
		})
	})

	Context("Mixed CONTAINS and NOT CONTAINS", func() {
		It("should find VMs with 'production' but not 'critical'", func() {
			names, err := queryVMs("labels contains 'production' and labels not contains 'critical'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-cache-01", "vm-db-02", "vm-web-02"}))
		})

		It("should find VMs with 'production' but not 'database'", func() {
			names, err := queryVMs("labels contains 'production' and labels not contains 'database'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-cache-01", "vm-web-01", "vm-web-02"}))
		})

		It("should find VMs without 'production' but with 'critical'", func() {
			names, err := queryVMs("labels not contains 'production' and labels contains 'critical'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-worker-02"}))
		})
	})

	Context("Complex expressions with parentheses", func() {
		It("should handle (contains OR contains) AND cluster", func() {
			names, err := queryVMs("(labels contains 'database' or labels contains 'cache') and cluster = 'prod-east'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-cache-01", "vm-db-01"}))
		})

		It("should handle cluster AND (contains OR contains)", func() {
			names, err := queryVMs("cluster = 'staging' and (labels contains 'critical' or labels contains 'temporary')")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-worker-02"}))
		})

		It("should handle contains AND (cluster OR cluster)", func() {
			names, err := queryVMs("labels contains 'production' and (cluster = 'prod-east' or cluster = 'prod-west')")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-cache-01", "vm-db-01", "vm-db-02", "vm-web-01", "vm-web-02"}))
		})
	})

	Context("Edge cases", func() {
		It("should handle VMs with empty labels array", func() {
			names, err := queryVMs("labels not contains 'production'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(ContainElement("vm-legacy"))
		})

		It("should not find VMs with empty labels when searching for any label", func() {
			names, err := queryVMs("labels contains 'production'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).NotTo(ContainElement("vm-legacy"))
		})

		It("should handle labels with special characters", func() {
			// Insert a VM with special characters in labels
			_, err := db.Exec(`INSERT INTO vms VALUES ('vm-special', 'test', ['prod-server', 'tier_1', 'wave.2', 'env:staging'])`)
			Expect(err).ToNot(HaveOccurred())

			names, err := queryVMs("labels contains 'prod-server'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(ContainElement("vm-special"))

			names, err = queryVMs("labels contains 'tier_1'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(ContainElement("vm-special"))

			names, err = queryVMs("labels contains 'wave.2'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(ContainElement("vm-special"))

			names, err = queryVMs("labels contains 'env:staging'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(ContainElement("vm-special"))
		})
	})

	Context("SQL Injection Prevention", func() {
		It("should handle parameterized queries securely", func() {
			// The filter uses parameterized queries, so values are passed as parameters
			// This test verifies that the table remains intact after various queries
			names, err := queryVMs("labels contains 'production'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(5))

			// Query with a value that would be dangerous if not parameterized
			names, err = queryVMs("labels contains 'DROP TABLE'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty()) // No VM has this label

			// Verify table still intact
			names, err = queryVMs("labels contains 'production'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(5))
		})
	})
})
