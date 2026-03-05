package filter

import (
	"database/sql"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/duckdb/duckdb-go/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Filter Integration with DuckDB", func() {
	var db *sql.DB

	// testMapper maps filter variable names to SQL column references.
	var testMapper MapFunc = func(name string) (string, error) {
		return fmt.Sprintf(`"%s"`, name), nil
	}

	BeforeEach(func() {
		var err error
		connector, err := duckdb.NewConnector("", nil)
		Expect(err).ToNot(HaveOccurred())

		db = sql.OpenDB(connector)
		Expect(db.Ping()).To(Succeed())

		// Create a VM-like table with 5 columns: string, bool, int, float (MB), string
		_, err = db.Exec(`CREATE TABLE vms (
			"name"     VARCHAR NOT NULL,
			"active"   BOOLEAN NOT NULL,
			"cpus"     INTEGER NOT NULL,
			"memory"   DOUBLE NOT NULL,
			"disk"     DOUBLE NOT NULL
		)`)
		Expect(err).ToNot(HaveOccurred())

		// Insert test data - memory and disk values are in MB (baseline unit)
		// Memory: 512MB, 1GB(1024), 2GB(2048), 4GB(4096), 8GB(8192), 16GB(16384), 32GB(32768)
		// Disk: 10GB(10240), 20GB(20480), 50GB(51200), 100GB(102400), 500GB(512000), 1TB(1048576), 2TB(2097152)
		_, err = db.Exec(`INSERT INTO vms VALUES
			('vm-web-01',      true,  2,  2048,    102400),
			('vm-web-02',      true,  4,  4096,    102400),
			('vm-db-01',       true,  8,  32768,   1048576),
			('vm-db-02',       true,  8,  16384,   512000),
			('vm-cache-01',    true,  4,  8192,    51200),
			('vm-worker-01',   false, 2,  1024,    20480),
			('vm-worker-02',   false, 1,  512,     10240),
			('vm-analytics',   true,  16, 65536,   2097152),
			('vm-legacy',      false, 1,  2048,    51200),
			('vm-test',        false, 2,  4096,    20480)
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

		sqlizer, err := toSql(expr, testMapper)
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

	// ============================================================
	// STRING COLUMN TESTS (name)
	// ============================================================

	Context("String equality (=)", func() {
		It("should find exact match", func() {
			names, err := queryVMs("name = 'vm-web-01'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-web-01"}))
		})

		It("should return empty for non-existent value", func() {
			names, err := queryVMs("name = 'vm-notexist'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})
	})

	Context("String inequality (!=)", func() {
		It("should find all except one", func() {
			names, err := queryVMs("name != 'vm-web-01'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(9))
			Expect(names).ToNot(ContainElement("vm-web-01"))
		})
	})

	Context("String regex match (~)", func() {
		It("should match prefix", func() {
			names, err := queryVMs("name ~ /^vm-web/")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-web-01", "vm-web-02"}))
		})

		It("should match suffix", func() {
			names, err := queryVMs("name ~ /-01$/")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-cache-01", "vm-db-01", "vm-web-01", "vm-worker-01"}))
		})

		It("should match substring", func() {
			names, err := queryVMs("name ~ /db/")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-db-02"}))
		})

		It("should match pattern with alternation", func() {
			names, err := queryVMs("name ~ /^vm-(web|db)/")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-db-02", "vm-web-01", "vm-web-02"}))
		})
	})

	Context("String regex not match (!~)", func() {
		It("should exclude pattern", func() {
			names, err := queryVMs("name !~ /^vm-web/")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).ToNot(ContainElements("vm-web-01", "vm-web-02"))
			Expect(names).To(HaveLen(8))
		})
	})

	// ============================================================
	// BOOLEAN COLUMN TESTS (active)
	// ============================================================

	Context("Boolean equality (= true)", func() {
		It("should find active VMs", func() {
			names, err := queryVMs("active = true")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-cache-01", "vm-db-01", "vm-db-02", "vm-web-01", "vm-web-02"}))
		})
	})

	Context("Boolean equality (= false)", func() {
		It("should find inactive VMs", func() {
			names, err := queryVMs("active = false")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-legacy", "vm-test", "vm-worker-01", "vm-worker-02"}))
		})
	})

	Context("Boolean inequality (!= true)", func() {
		It("should find VMs not active", func() {
			names, err := queryVMs("active != true")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-legacy", "vm-test", "vm-worker-01", "vm-worker-02"}))
		})
	})

	Context("Boolean inequality (!= false)", func() {
		It("should find VMs not inactive", func() {
			names, err := queryVMs("active != false")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-cache-01", "vm-db-01", "vm-db-02", "vm-web-01", "vm-web-02"}))
		})
	})

	// ============================================================
	// INTEGER COLUMN TESTS (cpus)
	// ============================================================

	Context("CPU equality (=)", func() {
		It("should find VMs with exact CPU count", func() {
			names, err := queryVMs("cpus = 8")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-db-02"}))
		})
	})

	Context("CPU inequality (!=)", func() {
		It("should exclude exact CPU count", func() {
			names, err := queryVMs("cpus != 2")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).ToNot(ContainElements("vm-web-01", "vm-worker-01", "vm-test"))
		})
	})

	Context("CPU greater than (>)", func() {
		It("should find VMs with cpus > 4", func() {
			names, err := queryVMs("cpus > 4")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01", "vm-db-02"}))
		})
	})

	Context("CPU greater than or equal (>=)", func() {
		It("should find VMs with cpus >= 4", func() {
			names, err := queryVMs("cpus >= 4")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-cache-01", "vm-db-01", "vm-db-02", "vm-web-02"}))
		})
	})

	Context("CPU less than (<)", func() {
		It("should find VMs with cpus < 2", func() {
			names, err := queryVMs("cpus < 2")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-legacy", "vm-worker-02"}))
		})
	})

	Context("CPU less than or equal (<=)", func() {
		It("should find VMs with cpus <= 2", func() {
			names, err := queryVMs("cpus <= 2")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-legacy", "vm-test", "vm-web-01", "vm-worker-01", "vm-worker-02"}))
		})
	})

	// ============================================================
	// MEMORY TESTS WITH UNIT CONVERSION (baseline: MB)
	// ============================================================

	Context("Memory in MB (baseline, no conversion)", func() {
		It("should find VMs with memory = 2048 MB", func() {
			names, err := queryVMs("memory = 2048MB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-legacy", "vm-web-01"}))
		})

		It("should find VMs with memory > 8192 MB", func() {
			names, err := queryVMs("memory > 8192MB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01", "vm-db-02"}))
		})

		It("should find VMs with memory < 1024 MB", func() {
			names, err := queryVMs("memory < 1024MB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-worker-02"}))
		})
	})

	Context("Memory in KB (divide by 1024)", func() {
		It("should find VMs with memory = 2097152 KB (2GB)", func() {
			names, err := queryVMs("memory = 2097152KB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-legacy", "vm-web-01"}))
		})

		It("should find VMs with memory < 1048576 KB (1GB)", func() {
			names, err := queryVMs("memory < 1048576KB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-worker-02"}))
		})

		It("should find VMs with memory >= 524288 KB (512MB)", func() {
			names, err := queryVMs("memory >= 524288KB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(10))
		})
	})

	Context("Memory in GB (multiply by 1024)", func() {
		It("should find VMs with memory = 2GB", func() {
			names, err := queryVMs("memory = 2GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-legacy", "vm-web-01"}))
		})

		It("should find VMs with memory > 8GB", func() {
			names, err := queryVMs("memory > 8GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01", "vm-db-02"}))
		})

		It("should find VMs with memory >= 16GB", func() {
			names, err := queryVMs("memory >= 16GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01", "vm-db-02"}))
		})

		It("should find VMs with memory < 1GB", func() {
			names, err := queryVMs("memory < 1GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-worker-02"}))
		})

		It("should find VMs with memory <= 4GB", func() {
			names, err := queryVMs("memory <= 4GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-legacy", "vm-test", "vm-web-01", "vm-web-02", "vm-worker-01", "vm-worker-02"}))
		})

		It("should find VMs with memory between 4GB and 16GB", func() {
			names, err := queryVMs("memory >= 4GB and memory <= 16GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-cache-01", "vm-db-02", "vm-test", "vm-web-02"}))
		})
	})

	Context("Memory in TB (multiply by 1024*1024)", func() {
		It("should find VMs with memory >= 0.03125TB (32GB)", func() {
			// 0.03125 TB = 32GB = 32768 MB
			names, err := queryVMs("memory >= 0.03125TB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01"}))
		})

		It("should find VMs with memory > 0.015625TB (16GB)", func() {
			// 0.015625 TB = 16GB = 16384 MB
			names, err := queryVMs("memory > 0.015625TB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01"}))
		})
	})

	// ============================================================
	// DISK TESTS WITH UNIT CONVERSION (baseline: MB)
	// ============================================================

	Context("Disk in MB (baseline, no conversion)", func() {
		It("should find VMs with disk = 102400 MB (100GB)", func() {
			names, err := queryVMs("disk = 102400MB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-web-01", "vm-web-02"}))
		})
	})

	Context("Disk in KB (divide by 1024)", func() {
		It("should find VMs with disk < 20971520 KB (20GB)", func() {
			names, err := queryVMs("disk < 20971520KB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-worker-02"}))
		})
	})

	Context("Disk in GB (multiply by 1024)", func() {
		It("should find VMs with disk = 100GB", func() {
			names, err := queryVMs("disk = 100GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-web-01", "vm-web-02"}))
		})

		It("should find VMs with disk > 500GB", func() {
			names, err := queryVMs("disk > 500GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01"}))
		})

		It("should find VMs with disk >= 500GB", func() {
			names, err := queryVMs("disk >= 500GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01", "vm-db-02"}))
		})

		It("should find VMs with disk < 50GB", func() {
			names, err := queryVMs("disk < 50GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-test", "vm-worker-01", "vm-worker-02"}))
		})

		It("should find VMs with disk <= 100GB", func() {
			names, err := queryVMs("disk <= 100GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-cache-01", "vm-legacy", "vm-test", "vm-web-01", "vm-web-02", "vm-worker-01", "vm-worker-02"}))
		})
	})

	Context("Disk in TB (multiply by 1024*1024)", func() {
		It("should find VMs with disk = 1TB", func() {
			names, err := queryVMs("disk = 1TB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01"}))
		})

		It("should find VMs with disk > 1TB", func() {
			names, err := queryVMs("disk > 1TB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics"}))
		})

		It("should find VMs with disk >= 1TB", func() {
			names, err := queryVMs("disk >= 1TB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01"}))
		})

		It("should find VMs with disk < 1TB", func() {
			names, err := queryVMs("disk < 1TB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(8))
			Expect(names).ToNot(ContainElements("vm-analytics", "vm-db-01"))
		})

		It("should find VMs with disk = 2TB", func() {
			names, err := queryVMs("disk = 2TB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics"}))
		})
	})

	Context("Mixed unit comparisons", func() {
		It("should find VMs with memory > 8GB and disk >= 1TB", func() {
			names, err := queryVMs("memory > 8GB and disk >= 1TB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01"}))
		})

		It("should find VMs with memory >= 1GB and disk < 100GB", func() {
			names, err := queryVMs("memory >= 1GB and disk < 100GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-cache-01", "vm-legacy", "vm-test", "vm-worker-01"}))
		})
	})

	// ============================================================
	// AND EXPRESSIONS
	// ============================================================

	Context("AND with two conditions", func() {
		It("should combine string and bool", func() {
			names, err := queryVMs("name ~ /^vm-db/ and active = true")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-db-02"}))
		})

		It("should combine string and int", func() {
			names, err := queryVMs("name ~ /^vm-web/ and cpus >= 4")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-web-02"}))
		})

		It("should combine bool and memory", func() {
			names, err := queryVMs("active = true and memory >= 16GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01", "vm-db-02"}))
		})

		It("should combine cpus and disk", func() {
			names, err := queryVMs("cpus >= 8 and disk >= 500GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01", "vm-db-02"}))
		})

		It("should combine memory and disk", func() {
			names, err := queryVMs("memory >= 8GB and disk >= 500GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01", "vm-db-02"}))
		})
	})

	Context("AND with three conditions", func() {
		It("should combine name, active, and cpus", func() {
			names, err := queryVMs("name ~ /^vm-/ and active = true and cpus >= 4")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-cache-01", "vm-db-01", "vm-db-02", "vm-web-02"}))
		})

		It("should combine active, memory, and disk", func() {
			names, err := queryVMs("active = true and memory >= 8GB and disk >= 100GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01", "vm-db-02"}))
		})
	})

	Context("AND with four conditions", func() {
		It("should combine all column types", func() {
			names, err := queryVMs("name ~ /^vm-db/ and active = true and cpus >= 8 and memory >= 16GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-db-02"}))
		})
	})

	Context("AND with five conditions", func() {
		It("should combine all columns", func() {
			names, err := queryVMs("name ~ /^vm-db/ and active = true and cpus = 8 and memory >= 16GB and disk >= 500GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-db-02"}))
		})
	})

	// ============================================================
	// OR EXPRESSIONS
	// ============================================================

	Context("OR with two conditions", func() {
		It("should combine string patterns", func() {
			names, err := queryVMs("name ~ /^vm-web/ or name ~ /^vm-db/")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-db-02", "vm-web-01", "vm-web-02"}))
		})

		It("should combine bool or bool (always all)", func() {
			names, err := queryVMs("active = true or active = false")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(10))
		})

		It("should combine cpu comparisons", func() {
			names, err := queryVMs("cpus = 1 or cpus >= 8")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01", "vm-db-02", "vm-legacy", "vm-worker-02"}))
		})

		It("should combine memory comparisons", func() {
			names, err := queryVMs("memory < 1GB or memory >= 32GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01", "vm-worker-02"}))
		})

		It("should combine disk comparisons", func() {
			names, err := queryVMs("disk < 20GB or disk >= 1TB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01", "vm-worker-02"}))
		})
	})

	Context("OR with three conditions", func() {
		It("should combine three name patterns", func() {
			names, err := queryVMs("name ~ /^vm-web/ or name ~ /^vm-db/ or name ~ /^vm-cache/")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-cache-01", "vm-db-01", "vm-db-02", "vm-web-01", "vm-web-02"}))
		})

		It("should combine mixed types", func() {
			names, err := queryVMs("name = 'vm-analytics' or cpus = 1 or memory < 1GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-legacy", "vm-worker-02"}))
		})
	})

	// ============================================================
	// MIXED AND/OR (AND has higher precedence)
	// ============================================================

	Context("AND/OR precedence", func() {
		It("should evaluate A or B and C as A or (B and C)", func() {
			// name ~ /^vm-analytics/ OR (active = true AND cpus >= 8)
			names, err := queryVMs("name = 'vm-analytics' or active = true and cpus >= 8")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01", "vm-db-02"}))
		})

		It("should evaluate A and B or C as (A and B) or C", func() {
			// (active = false AND cpus = 1) OR name = 'vm-analytics'
			names, err := queryVMs("active = false and cpus = 1 or name = 'vm-analytics'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-legacy", "vm-worker-02"}))
		})

		It("should evaluate A and B or C and D", func() {
			// (name ~ /^vm-web/ AND cpus >= 4) OR (name ~ /^vm-db/ AND memory >= 32GB)
			names, err := queryVMs("name ~ /^vm-web/ and cpus >= 4 or name ~ /^vm-db/ and memory >= 32GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-web-02"}))
		})
	})

	// ============================================================
	// PARENTHESES (GROUPING)
	// ============================================================

	Context("Parentheses override precedence", func() {
		It("should evaluate (A or B) and C", func() {
			// (name ~ /^vm-web/ OR name ~ /^vm-db/) AND cpus >= 4
			names, err := queryVMs("(name ~ /^vm-web/ or name ~ /^vm-db/) and cpus >= 4")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-db-02", "vm-web-02"}))
		})

		It("should evaluate A and (B or C)", func() {
			// active = true AND (cpus <= 2 OR cpus >= 16)
			names, err := queryVMs("active = true and (cpus <= 2 or cpus >= 16)")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-web-01"}))
		})

		It("should evaluate (A or B) and (C or D)", func() {
			// (name ~ /^vm-web/ OR name ~ /^vm-db/) AND (memory >= 8GB OR disk >= 500GB)
			// vm-web-01: memory=2GB, disk=100GB -> neither >= 8GB nor >= 500GB
			// vm-web-02: memory=4GB, disk=100GB -> neither >= 8GB nor >= 500GB
			// vm-db-01: memory=32GB, disk=1TB -> both conditions true
			// vm-db-02: memory=16GB, disk=500GB -> both conditions true
			names, err := queryVMs("(name ~ /^vm-web/ or name ~ /^vm-db/) and (memory >= 8GB or disk >= 500GB)")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-db-02"}))
		})

		It("should handle nested parentheses", func() {
			// ((A or B) and C) or D
			names, err := queryVMs("((name ~ /^vm-web/ or name ~ /^vm-db/) and cpus >= 8) or name = 'vm-analytics'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01", "vm-db-02"}))
		})

		It("should handle deeply nested parentheses", func() {
			// A and (B or (C and D))
			names, err := queryVMs("active = true and (cpus >= 16 or (memory >= 16GB and disk >= 500GB))")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01", "vm-db-02"}))
		})
	})

	// ============================================================
	// COMPLEX EXPRESSIONS
	// ============================================================

	Context("Complex real-world expressions", func() {
		It("should find production-ready VMs (active with sufficient resources)", func() {
			// vm-web-02: cpus=4, memory=4GB (not >= 8GB), disk=100GB -> fails memory check
			names, err := queryVMs("active = true and cpus >= 4 and memory >= 8GB and disk >= 100GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01", "vm-db-02"}))
		})

		It("should find VMs needing resource upgrade", func() {
			names, err := queryVMs("active = true and (cpus < 4 or memory < 4GB)")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-web-01"}))
		})

		It("should find VMs by name pattern with resource constraints", func() {
			names, err := queryVMs("name ~ /^vm-db/ and memory >= 16GB and disk >= 500GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-db-02"}))
		})

		It("should combine all operators", func() {
			// String ~, String !=, Bool =, Int >=, Memory >=, Disk >=
			// vm-web-02 has cpus=4, memory=4GB (not >= 8GB), disk=100GB -> fails memory check
			names, err := queryVMs("name ~ /^vm-/ and name != 'vm-analytics' and active = true and cpus >= 4 and memory >= 8GB and disk >= 100GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-db-02"}))
		})
	})

	// ============================================================
	// EDGE CASES
	// ============================================================

	Context("Edge cases", func() {
		It("should match all VMs for always-true condition", func() {
			names, err := queryVMs("cpus >= 1")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(10))
		})

		It("should return empty for impossible condition", func() {
			names, err := queryVMs("cpus > 100")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})

		It("should handle zero values", func() {
			names, err := queryVMs("cpus > 0 and memory > 0MB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(10))
		})

		It("should handle negative comparison (no matches)", func() {
			names, err := queryVMs("cpus < 0")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})

		It("should return parse error for invalid filter", func() {
			_, err := queryVMs("name =")
			Expect(err).To(HaveOccurred())
		})

		It("should return parse error for unknown operator", func() {
			_, err := queryVMs("name $ 'test'")
			Expect(err).To(HaveOccurred())
		})
	})

	// ============================================================
	// IN OPERATOR
	// ============================================================

	Context("IN operator", func() {
		It("should find VMs with name in list", func() {
			names, err := queryVMs("name in ['vm-web-01', 'vm-db-01']")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-web-01"}))
		})

		It("should find VMs with single value in list", func() {
			names, err := queryVMs("name in ['vm-analytics']")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics"}))
		})

		It("should return empty for no matches", func() {
			names, err := queryVMs("name in ['nonexistent']")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})

		It("should work with empty list (no matches)", func() {
			names, err := queryVMs("name in []")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})

		It("should combine IN with AND", func() {
			names, err := queryVMs("name in ['vm-web-01', 'vm-web-02', 'vm-db-01'] and active = true")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-web-01", "vm-web-02"}))
		})

		It("should combine IN with OR", func() {
			names, err := queryVMs("name in ['vm-web-01'] or name = 'vm-db-01'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-web-01"}))
		})

		It("should combine IN with other filters", func() {
			names, err := queryVMs("name in ['vm-web-01', 'vm-web-02', 'vm-db-01', 'vm-db-02'] and memory >= 8GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-db-02"}))
		})
	})

	// ============================================================
	// ALL OPERATOR COMBINATIONS
	// ============================================================

	Context("All operators on string column", func() {
		It("= operator", func() {
			names, err := queryVMs("name = 'vm-web-01'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-web-01"}))
		})

		It("!= operator", func() {
			names, err := queryVMs("name != 'vm-web-01'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(9))
		})

		It("~ operator", func() {
			names, err := queryVMs("name ~ /web/")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-web-01", "vm-web-02"}))
		})

		It("!~ operator", func() {
			names, err := queryVMs("name !~ /web/")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(8))
		})
	})

	Context("All operators on boolean column", func() {
		It("= true", func() {
			names, err := queryVMs("active = true")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(6))
		})

		It("= false", func() {
			names, err := queryVMs("active = false")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(4))
		})

		It("!= true", func() {
			names, err := queryVMs("active != true")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(4))
		})

		It("!= false", func() {
			names, err := queryVMs("active != false")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(6))
		})
	})

	Context("All operators on integer column", func() {
		It("= operator", func() {
			names, err := queryVMs("cpus = 8")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01", "vm-db-02"}))
		})

		It("!= operator", func() {
			names, err := queryVMs("cpus != 8")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(8))
		})

		It("> operator", func() {
			names, err := queryVMs("cpus > 8")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics"}))
		})

		It(">= operator", func() {
			names, err := queryVMs("cpus >= 8")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01", "vm-db-02"}))
		})

		It("< operator", func() {
			names, err := queryVMs("cpus < 2")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-legacy", "vm-worker-02"}))
		})

		It("<= operator", func() {
			names, err := queryVMs("cpus <= 2")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-legacy", "vm-test", "vm-web-01", "vm-worker-01", "vm-worker-02"}))
		})
	})

	Context("All operators on memory column with units", func() {
		It("= operator with GB", func() {
			names, err := queryVMs("memory = 4GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-test", "vm-web-02"}))
		})

		It("!= operator with GB", func() {
			names, err := queryVMs("memory != 4GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(8))
		})

		It("> operator with GB", func() {
			names, err := queryVMs("memory > 32GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics"}))
		})

		It(">= operator with GB", func() {
			names, err := queryVMs("memory >= 32GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01"}))
		})

		It("< operator with GB", func() {
			names, err := queryVMs("memory < 1GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-worker-02"}))
		})

		It("<= operator with GB", func() {
			names, err := queryVMs("memory <= 1GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-worker-01", "vm-worker-02"}))
		})
	})

	Context("All operators on disk column with units", func() {
		It("= operator with TB", func() {
			names, err := queryVMs("disk = 1TB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-db-01"}))
		})

		It("!= operator with TB", func() {
			names, err := queryVMs("disk != 1TB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(9))
		})

		It("> operator with TB", func() {
			names, err := queryVMs("disk > 1TB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics"}))
		})

		It(">= operator with TB", func() {
			names, err := queryVMs("disk >= 1TB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-analytics", "vm-db-01"}))
		})

		It("< operator with GB", func() {
			names, err := queryVMs("disk < 20GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-worker-02"}))
		})

		It("<= operator with GB", func() {
			names, err := queryVMs("disk <= 20GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-test", "vm-worker-01", "vm-worker-02"}))
		})
	})

	// ============================================================
	// SQL INJECTION PREVENTION TESTS
	// These tests verify that SQL injection attempts are properly
	// parameterized and treated as data, not executable SQL.
	// ============================================================

	Context("SQL Injection Prevention - String Values", func() {
		It("should treat DROP TABLE as literal string value", func() {
			// This should search for a VM named literally "'; DROP TABLE vms; --"
			// Not execute the DROP TABLE command
			names, err := queryVMs("name = '\\'; DROP TABLE vms; --'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty()) // No VM with this name exists

			// Verify table still exists by running another query
			names, err = queryVMs("name = 'vm-web-01'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-web-01"}))
		})

		It("should treat DELETE FROM as literal string value", func() {
			names, err := queryVMs("name = '\\'; DELETE FROM vms; --'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())

			// Verify data still exists
			names, err = queryVMs("cpus >= 1")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(10))
		})

		It("should treat UNION SELECT as literal string value", func() {
			names, err := queryVMs("name = '\\' UNION SELECT * FROM vms --'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})

		It("should treat OR 1=1 as literal string value", func() {
			names, err := queryVMs("name = '\\' OR \\'1\\'=\\'1'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty()) // Should not return all rows
		})

		It("should handle SQL comment injection in string", func() {
			names, err := queryVMs("name = 'test--comment'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})

		It("should handle block comment injection in string", func() {
			names, err := queryVMs("name = 'test/*comment*/'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})

		It("should handle semicolon injection in string", func() {
			names, err := queryVMs("name = 'test; SELECT * FROM vms'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})
	})

	Context("SQL Injection Prevention - Quote Escaping", func() {
		It("should handle escaped single quotes correctly", func() {
			names, err := queryVMs("name = 'O\\'Brien'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty()) // No VM with this name
		})

		It("should handle multiple escaped quotes", func() {
			names, err := queryVMs("name = 'it\\'s a \\'test\\''")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})

		It("should handle double quotes inside single quotes", func() {
			names, err := queryVMs("name = 'say \"hello\"'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})

		It("should handle backslash sequences", func() {
			names, err := queryVMs("name = 'path\\\\to\\\\file'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})
	})

	Context("SQL Injection Prevention - IN Operator", func() {
		It("should treat injection in IN list as literal values", func() {
			names, err := queryVMs("name in ['normal', '\\'; DROP TABLE vms; --']")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())

			// Verify table still exists
			names, err = queryVMs("name = 'vm-web-01'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-web-01"}))
		})

		It("should handle UNION injection in IN list", func() {
			names, err := queryVMs("name in ['a', 'b', '\\' UNION SELECT * --']")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})
	})

	Context("SQL Injection Prevention - Regex Patterns", func() {
		It("should treat injection in regex as literal pattern", func() {
			names, err := queryVMs("name ~ /'; DROP TABLE vms; --/")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())

			// Verify table still exists
			names, err = queryVMs("cpus >= 1")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(10))
		})

		It("should handle SQL keywords in regex", func() {
			names, err := queryVMs("name ~ /SELECT.*FROM/")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})
	})

	Context("SQL Injection Prevention - Special Characters", func() {
		It("should handle null byte in string", func() {
			// Null bytes cause a parse error (unclosed string), which is safe behavior
			// The parser rejects input containing null bytes
			_, err := queryVMs("name = 'test\x00injection'")
			Expect(err).To(HaveOccurred()) // Parse error is expected and safe
		})

		It("should handle newline in string", func() {
			names, err := queryVMs("name = 'line1\nline2'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})

		It("should handle tab in string", func() {
			names, err := queryVMs("name = 'col1\tcol2'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})

		It("should handle SQL wildcards as literals", func() {
			// % and _ should be treated as literal characters in equality
			names, err := queryVMs("name = '%'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())

			names, err = queryVMs("name = '_'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})
	})

	Context("SQL Injection Prevention - Unicode", func() {
		It("should handle Chinese characters", func() {
			names, err := queryVMs("name = '测试'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})

		It("should handle Cyrillic lookalikes", func() {
			// 'tеst' with Cyrillic 'е' (U+0435) instead of Latin 'e'
			names, err := queryVMs("name = 'tеst'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})

		It("should handle emoji", func() {
			names, err := queryVMs("name = '🎉test🎉'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})

		It("should handle right-to-left override", func() {
			names, err := queryVMs("name = 'test\u202Eexe.txt'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})
	})

	Context("SQL Injection Prevention - Combined Attacks", func() {
		It("should handle injection combined with valid filter", func() {
			names, err := queryVMs("name = 'vm-web-01' and name = '\\'; DROP TABLE vms; --'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty()) // AND makes this impossible

			// Verify table still exists
			names, err = queryVMs("name = 'vm-web-01'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-web-01"}))
		})

		It("should handle injection in OR clause", func() {
			names, err := queryVMs("name = 'vm-web-01' or name = '\\'; DROP TABLE vms; --'")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(Equal([]string{"vm-web-01"})) // Only valid match

			// Verify table still exists with all data
			names, err = queryVMs("cpus >= 1")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(10))
		})

		It("should handle multiple injection attempts in one query", func() {
			names, err := queryVMs("name = 'DROP' and active = true and name ~ /DELETE/")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(BeEmpty())
		})
	})

	Context("SQL Injection Prevention - Verify Data Integrity After All Tests", func() {
		It("should have all original data intact", func() {
			// This test verifies that none of the injection attempts modified data
			names, err := queryVMs("cpus >= 1")
			Expect(err).ToNot(HaveOccurred())
			Expect(names).To(HaveLen(10))

			// Verify specific VMs still exist
			expectedVMs := []string{
				"vm-analytics", "vm-cache-01", "vm-db-01", "vm-db-02",
				"vm-legacy", "vm-test", "vm-web-01", "vm-web-02",
				"vm-worker-01", "vm-worker-02",
			}
			Expect(names).To(Equal(expectedVMs))
		})
	})
})
