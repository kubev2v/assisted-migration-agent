package filter

import (
	"database/sql"
	"fmt"

	"github.com/duckdb/duckdb-go/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Filter Integration with DuckDB", func() {
	var db *sql.DB

	BeforeEach(func() {
		var err error
		connector, err := duckdb.NewConnector("", nil)
		Expect(err).ToNot(HaveOccurred())

		db = sql.OpenDB(connector)
		Expect(db.Ping()).To(Succeed())

		createSchema(db)
		insertTestData(db)
	})

	AfterEach(func() {
		if db != nil {
			db.Close()
		}
	})

	queryVMs := func(filterExpr string) ([]string, error) {
		expr, err := Parse([]byte(filterExpr))
		if err != nil {
			return nil, err
		}

		query := `SELECT DISTINCT i."VM ID" AS id
			FROM vinfo i
			LEFT JOIN vcpu c ON i."VM ID" = c."VM ID"
			LEFT JOIN vmemory m ON i."VM ID" = m."VM ID"
			LEFT JOIN vdisk dk ON i."VM ID" = dk."VM ID"
			LEFT JOIN vdatastore ds
				ON ds."Name" = regexp_extract(COALESCE(dk."Path", dk."Disk Path"), '\[([^\]]+)\]', 1)
			LEFT JOIN vnetwork n ON i."VM ID" = n."VM ID"
			LEFT JOIN concerns con ON i."VM ID" = con."VM_ID"
			WHERE ` + expr.Sql() + `
			ORDER BY i."VM ID"`

		rows, err := db.Query(query)
		if err != nil {
			return nil, fmt.Errorf("query failed: %w\nFilter SQL: %s", err, expr.Sql())
		}
		defer rows.Close()

		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		return ids, rows.Err()
	}

	Context("String equality on vinfo columns", func() {
		It("should find VM by name", func() {
			ids, err := queryVMs("vm = 'prod-web-01'")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-001"}))
		})

		It("should find VM by ID", func() {
			ids, err := queryVMs("vm_id = 'vm-003'")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003"}))
		})

		It("should find VMs by cluster", func() {
			ids, err := queryVMs("cluster = 'Cluster-A'")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-001", "vm-002", "vm-006", "vm-007", "vm-010"}))
		})

		It("should find VMs by datacenter", func() {
			ids, err := queryVMs("datacenter = 'DC2'")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-004", "vm-005", "vm-008"}))
		})

		It("should find VMs by powerstate", func() {
			ids, err := queryVMs("powerstate = 'poweredOff'")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-005", "vm-006", "vm-007"}))
		})

		It("should find VMs not in cluster", func() {
			ids, err := queryVMs("cluster != 'Cluster-A'")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003", "vm-004", "vm-005", "vm-008", "vm-009"}))
		})

		It("should return empty for non-existent name", func() {
			ids, err := queryVMs("vm = 'does-not-exist'")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(BeEmpty())
		})
	})

	Context("Boolean equality", func() {
		It("should find template VMs", func() {
			ids, err := queryVMs("template = true")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-006", "vm-007"}))
		})

		It("should find non-template VMs", func() {
			ids, err := queryVMs("template = false")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(HaveLen(8))
			Expect(ids).ToNot(ContainElements("vm-006", "vm-007"))
		})

		It("should find VMs with CPU hot-add enabled", func() {
			ids, err := queryVMs("cpu_hot_add = true")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-001", "vm-002", "vm-003", "vm-008", "vm-010"}))
		})

		It("should find VMs with memory hot-add enabled", func() {
			ids, err := queryVMs("memory_hot_add = true")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-001", "vm-002", "vm-003", "vm-008", "vm-010"}))
		})
	})

	Context("Numeric comparisons", func() {
		It("should find VMs with 8 CPUs", func() {
			ids, err := queryVMs("cpus = 8")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003", "vm-008"}))
		})

		It("should find VMs with more than 4 CPUs", func() {
			ids, err := queryVMs("cpus > 4")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003", "vm-008"}))
		})

		It("should find VMs with at least 4 CPUs", func() {
			ids, err := queryVMs("cpus >= 4")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-001", "vm-002", "vm-003", "vm-007", "vm-008", "vm-010"}))
		})

		It("should find VMs with less than 4 CPUs", func() {
			ids, err := queryVMs("cpus < 4")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-004", "vm-005", "vm-006", "vm-009"}))
		})

		It("should find VMs with memory over 8192 MB", func() {
			ids, err := queryVMs("memory > 8192")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003", "vm-008"}))
		})
	})

	Context("Quantity comparisons with unit conversion", func() {
		It("should find VMs with disks larger than 500 GB", func() {
			ids, err := queryVMs("capacity_mib > 500GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003"}))
		})

		It("should find VMs with disks at least 500 GB", func() {
			ids, err := queryVMs("capacity_mib >= 500GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003", "vm-008"}))
		})

		It("should find VMs with disks smaller than 50 GB", func() {
			ids, err := queryVMs("capacity_mib < 50GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-006", "vm-007", "vm-009"}))
		})

		It("should find VMs with memory at least 16 GB", func() {
			ids, err := queryVMs("memory >= 16GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003", "vm-008"}))
		})
	})

	Context("Regex matching", func() {
		It("should find VMs with name starting with prod-", func() {
			ids, err := queryVMs("vm ~ /^prod-/")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-001", "vm-002", "vm-003", "vm-010"}))
		})

		It("should find VMs with name containing web", func() {
			ids, err := queryVMs("vm ~ /web/")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-001", "vm-002", "vm-004"}))
		})

		It("should find VMs with name NOT starting with prod-", func() {
			ids, err := queryVMs("vm !~ /^prod-/")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(HaveLen(6))
			Expect(ids).ToNot(ContainElements("vm-001", "vm-002", "vm-003", "vm-010"))
		})

		It("should find VMs running RHEL", func() {
			ids, err := queryVMs("os_config ~ /Red Hat/")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-001", "vm-002", "vm-003", "vm-006", "vm-008"}))
		})

		It("should find VMs running Windows", func() {
			ids, err := queryVMs("os_config ~ /Windows/")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-007", "vm-009"}))
		})

		It("should find VMs with IPs in 192.168.1.x subnet", func() {
			ids, err := queryVMs("primary_ip_address ~ /^192\\.168\\.1\\./")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-001", "vm-002", "vm-003", "vm-009", "vm-010"}))
		})
	})

	Context("Cross-table filtering (disk, network, concerns)", func() {
		It("should find VMs with thin-provisioned disks", func() {
			ids, err := queryVMs("thin = true")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-001", "vm-002", "vm-004", "vm-005", "vm-006", "vm-007", "vm-008", "vm-010"}))
		})

		It("should find VMs on a specific network", func() {
			ids, err := queryVMs("network = 'Backup Network'")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003"}))
		})

		It("should find VMs with e1000 NICs", func() {
			ids, err := queryVMs("nic_type ~ /e1000/")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-007", "vm-009"}))
		})

		It("should find VMs with critical concerns", func() {
			ids, err := queryVMs("category = 'Critical'")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003"}))
		})

		It("should find VMs with any concern", func() {
			ids, err := queryVMs("category ~ /./")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003", "vm-009"}))
		})

		It("should find VMs on datastore DS2", func() {
			ids, err := queryVMs("datastore_name = 'DS2'")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003", "vm-008"}))
		})
	})

	Context("AND expressions", func() {
		It("should find production DB VMs", func() {
			ids, err := queryVMs("vm ~ /^prod-/ and vm ~ /db/")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003"}))
		})

		It("should find non-template VMs with 8 CPUs", func() {
			ids, err := queryVMs("template = false and cpus = 8")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003", "vm-008"}))
		})

		It("should find powered-on VMs in DC1 with CPU hot-add", func() {
			ids, err := queryVMs("powerstate = 'poweredOn' and datacenter = 'DC1' and cpu_hot_add = true")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-001", "vm-002", "vm-003", "vm-010"}))
		})

		It("should find VMs with large disks on Cluster-B", func() {
			ids, err := queryVMs("cluster = 'Cluster-B' and capacity_mib >= 500GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003"}))
		})
	})

	Context("OR expressions", func() {
		It("should find templates or VMs with 8 CPUs", func() {
			ids, err := queryVMs("template = true or cpus = 8")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003", "vm-006", "vm-007", "vm-008"}))
		})

		It("should find VMs on DC1 or DC2", func() {
			ids, err := queryVMs("datacenter = 'DC1' or datacenter = 'DC2'")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(HaveLen(10))
		})

		It("should find VMs running RHEL or Windows", func() {
			ids, err := queryVMs("os_config ~ /Red Hat/ or os_config ~ /Windows/")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-001", "vm-002", "vm-003", "vm-006", "vm-007", "vm-008", "vm-009"}))
		})
	})

	Context("AND/OR precedence", func() {
		It("should respect AND having higher precedence than OR", func() {
			// prod OR (staging AND poweredOn)
			ids, err := queryVMs("vm ~ /^prod-/ or vm ~ /^staging-/ and powerstate = 'poweredOn'")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-001", "vm-002", "vm-003", "vm-004", "vm-010"}))
		})
	})

	Context("Parentheses grouping", func() {
		It("should group OR before AND", func() {
			ids, err := queryVMs("(os_config ~ /Red Hat/ or os_config ~ /Windows/) and template = false")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-001", "vm-002", "vm-003", "vm-008", "vm-009"}))
		})

		It("should handle nested parentheses", func() {
			ids, err := queryVMs("(template = true or (vm ~ /runner/ and cpus >= 8))")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-006", "vm-007", "vm-008"}))
		})

		It("should handle complex grouped expression", func() {
			ids, err := queryVMs("(datacenter = 'DC1' or datacenter = 'DC2') and (vm ~ /web/ or vm ~ /db/) and powerstate = 'poweredOn'")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-001", "vm-002", "vm-003", "vm-004"}))
		})
	})

	Context("Cross-table combined filters", func() {
		It("should find production VMs with large disks", func() {
			ids, err := queryVMs("vm ~ /^prod-/ and capacity_mib >= 500GB")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003"}))
		})

		It("should find VMs with critical concerns that are powered on", func() {
			ids, err := queryVMs("category = 'Critical' and powerstate = 'poweredOn'")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003"}))
		})

		It("should find VMs with e1000 NICs or disks at least 1 TB", func() {
			ids, err := queryVMs("nic_type ~ /e1000/ or capacity_mib >= 1TB")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003", "vm-007", "vm-009"}))
		})

		It("should find non-template powered-on VMs on DS2 with CPU hot-add", func() {
			ids, err := queryVMs("template = false and powerstate = 'poweredOn' and datastore_name = 'DS2' and cpu_hot_add = true")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-003", "vm-008"}))
		})
	})

	Context("Edge cases", func() {
		It("should match all VMs for always-true condition", func() {
			ids, err := queryVMs("cpus >= 1")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(HaveLen(10))
		})

		It("should return empty for impossible condition", func() {
			ids, err := queryVMs("cpus > 100")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(BeEmpty())
		})

		It("should handle deeply nested parentheses", func() {
			ids, err := queryVMs("((template = false and cpus >= 4) or (template = true and cpus = 2))")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-001", "vm-002", "vm-003", "vm-006", "vm-008", "vm-010"}))
		})

		It("should handle complex regex patterns", func() {
			ids, err := queryVMs("vm ~ /^(prod|staging)-/")
			Expect(err).ToNot(HaveOccurred())
			Expect(ids).To(Equal([]string{"vm-001", "vm-002", "vm-003", "vm-004", "vm-010"}))
		})

		It("should return parse error for invalid filter", func() {
			_, err := queryVMs("vm =")
			Expect(err).To(HaveOccurred())
		})
	})
})

// createSchema creates the vSphere inventory tables matching the real schema.
// Column names are identical to what columnMap maps to.
func createSchema(db *sql.DB) {
	stmts := []string{
		`CREATE TABLE vinfo (
			"VM ID"       VARCHAR PRIMARY KEY,
			"VM"          VARCHAR NOT NULL,
			"Powerstate"  VARCHAR NOT NULL,
			"Cluster"     VARCHAR,
			"Datacenter"  VARCHAR,
			"CPUs"        INTEGER NOT NULL,
			"Memory"      INTEGER NOT NULL,
			"Template"    BOOLEAN NOT NULL,
			"OS according to the configuration file" VARCHAR,
			"Firmware"    VARCHAR,
			"Host"        VARCHAR,
			"Primary IP Address" VARCHAR,
			"HW version"  VARCHAR
		)`,
		`CREATE TABLE vcpu (
			"VM ID"      VARCHAR NOT NULL,
			"Hot Add"    BOOLEAN NOT NULL,
			"Hot Remove" BOOLEAN NOT NULL,
			"Sockets"    INTEGER NOT NULL,
			"Cores p/s"  INTEGER NOT NULL
		)`,
		`CREATE TABLE vmemory (
			"VM ID"     VARCHAR NOT NULL,
			"Hot Add"   BOOLEAN NOT NULL,
			"Ballooned" INTEGER NOT NULL
		)`,
		`CREATE TABLE vdisk (
			"VM ID"        VARCHAR NOT NULL,
			"Disk Key"     INTEGER,
			"Disk Path"    VARCHAR,
			"Path"         VARCHAR,
			"Capacity MiB" DOUBLE NOT NULL,
			"Thin"         BOOLEAN,
			"Controller"   VARCHAR,
			"Label"        VARCHAR
		)`,
		`CREATE TABLE vdatastore (
			"Name"      VARCHAR NOT NULL,
			"Object ID" VARCHAR
		)`,
		`CREATE TABLE vnetwork (
			"VM ID"       VARCHAR NOT NULL,
			"Network"     VARCHAR,
			"Mac Address" VARCHAR,
			"Type"        VARCHAR
		)`,
		`CREATE TABLE concerns (
			"VM_ID"      VARCHAR NOT NULL,
			"Concern_ID" VARCHAR NOT NULL,
			"Label"      VARCHAR,
			"Category"   VARCHAR,
			"Assessment" VARCHAR
		)`,
	}
	for _, s := range stmts {
		_, err := db.Exec(s)
		ExpectWithOffset(1, err).ToNot(HaveOccurred())
	}
}

// insertTestData populates the schema with 10 VMs spanning different clusters,
// datacenters, OS types, and power states. Some VMs have multiple disks, NICs,
// or concerns to exercise the cartesian-product deduplication.
//
// Data summary:
//
//	vm-001  prod-web-01      poweredOn   Cluster-A  DC1  4cpu  8192MB   RHEL9     thin disks on DS1
//	vm-002  prod-web-02      poweredOn   Cluster-A  DC1  4cpu  8192MB   RHEL9     thin disk on DS1
//	vm-003  prod-db-01       poweredOn   Cluster-B  DC1  8cpu  32768MB  RHEL9     thick disks on DS2, 2 NICs, critical concern
//	vm-004  staging-web-01   poweredOn   Cluster-C  DC2  2cpu  4096MB   Ubuntu    thin disk on DS1
//	vm-005  dev-db-01        poweredOff  Cluster-C  DC2  2cpu  4096MB   Ubuntu    thin disk on DS1
//	vm-006  template-rhel9   poweredOff  Cluster-A  DC1  2cpu  2048MB   RHEL9     thin disk on DS1  (template)
//	vm-007  template-win2022 poweredOff  Cluster-A  DC1  4cpu  4096MB   Win2022   thin disk on DS1  (template, e1000e NIC)
//	vm-008  ci-runner-01     poweredOn   Cluster-D  DC2  8cpu  16384MB  RHEL8     thin disk on DS2, 2 NICs
//	vm-009  legacy-app-01    poweredOn   Cluster-B  DC1  1cpu  2048MB   Win2019   thick disk on DS1, e1000 NIC, 2 concerns
//	vm-010  prod-cache-01    poweredOn   Cluster-A  DC1  4cpu  8192MB   RHEL9     thin disk on DS1
func insertTestData(db *sql.DB) {
	exec := func(query string) {
		_, err := db.Exec(query)
		ExpectWithOffset(1, err).ToNot(HaveOccurred())
	}

	// vinfo
	exec(`INSERT INTO vinfo VALUES
		('vm-001','prod-web-01','poweredOn','Cluster-A','DC1',4,8192,false,'Red Hat Enterprise Linux 9','BIOS','esxi-01','192.168.1.10','vmx-19'),
		('vm-002','prod-web-02','poweredOn','Cluster-A','DC1',4,8192,false,'Red Hat Enterprise Linux 9','BIOS','esxi-01','192.168.1.11','vmx-19'),
		('vm-003','prod-db-01','poweredOn','Cluster-B','DC1',8,32768,false,'Red Hat Enterprise Linux 9','EFI','esxi-02','192.168.1.20','vmx-19'),
		('vm-004','staging-web-01','poweredOn','Cluster-C','DC2',2,4096,false,'Ubuntu 22.04','BIOS','esxi-03','192.168.2.10','vmx-17'),
		('vm-005','dev-db-01','poweredOff','Cluster-C','DC2',2,4096,false,'Ubuntu 22.04','BIOS','esxi-03','192.168.2.20','vmx-17'),
		('vm-006','template-rhel9','poweredOff','Cluster-A','DC1',2,2048,true,'Red Hat Enterprise Linux 9','BIOS','esxi-01',NULL,'vmx-19'),
		('vm-007','template-win2022','poweredOff','Cluster-A','DC1',4,4096,true,'Microsoft Windows Server 2022','EFI','esxi-01',NULL,'vmx-19'),
		('vm-008','ci-runner-01','poweredOn','Cluster-D','DC2',8,16384,false,'Red Hat Enterprise Linux 8','BIOS','esxi-04','192.168.3.10','vmx-19'),
		('vm-009','legacy-app-01','poweredOn','Cluster-B','DC1',1,2048,false,'Microsoft Windows Server 2019','BIOS','esxi-02','192.168.1.30','vmx-14'),
		('vm-010','prod-cache-01','poweredOn','Cluster-A','DC1',4,8192,false,'Red Hat Enterprise Linux 9','BIOS','esxi-01','192.168.1.31','vmx-19')
	`)

	// vcpu
	exec(`INSERT INTO vcpu VALUES
		('vm-001',true,false,2,2),
		('vm-002',true,false,2,2),
		('vm-003',true,true,2,4),
		('vm-004',false,false,1,2),
		('vm-005',false,false,1,2),
		('vm-006',false,false,1,2),
		('vm-007',false,false,2,2),
		('vm-008',true,true,2,4),
		('vm-009',false,false,1,1),
		('vm-010',true,false,2,2)
	`)

	// vmemory
	exec(`INSERT INTO vmemory VALUES
		('vm-001',true,0),
		('vm-002',true,0),
		('vm-003',true,512),
		('vm-004',false,0),
		('vm-005',false,0),
		('vm-006',false,0),
		('vm-007',false,0),
		('vm-008',true,256),
		('vm-009',false,128),
		('vm-010',true,0)
	`)

	// vdisk — vm-001 and vm-003 have two disks each
	exec(`INSERT INTO vdisk ("VM ID","Disk Key","Disk Path","Path","Capacity MiB","Thin","Controller","Label") VALUES
		('vm-001',2000,'[DS1] prod-web-01/disk.vmdk',NULL,102400,true,'SCSI 0','Hard disk 1'),
		('vm-001',2001,'[DS1] prod-web-01/disk_1.vmdk',NULL,51200,true,'SCSI 0','Hard disk 2'),
		('vm-002',2000,'[DS1] prod-web-02/disk.vmdk',NULL,102400,true,'SCSI 0','Hard disk 1'),
		('vm-003',2000,'[DS2] prod-db-01/disk.vmdk',NULL,512000,false,'SCSI 0','Hard disk 1'),
		('vm-003',2001,'[DS2] prod-db-01/disk_1.vmdk',NULL,1048576,false,'SCSI 0','Hard disk 2'),
		('vm-004',2000,'[DS1] staging-web-01/disk.vmdk',NULL,51200,true,'SCSI 0','Hard disk 1'),
		('vm-005',2000,'[DS1] dev-db-01/disk.vmdk',NULL,51200,true,'SCSI 0','Hard disk 1'),
		('vm-006',2000,'[DS1] template-rhel9/disk.vmdk',NULL,20480,true,'SCSI 0','Hard disk 1'),
		('vm-007',2000,'[DS1] template-win2022/disk.vmdk',NULL,40960,true,'SCSI 0','Hard disk 1'),
		('vm-008',2000,'[DS2] ci-runner-01/disk.vmdk',NULL,512000,true,'SCSI 0','Hard disk 1'),
		('vm-009',2000,'[DS1] legacy-app-01/disk.vmdk',NULL,25600,false,'SCSI 0','Hard disk 1'),
		('vm-010',2000,'[DS1] prod-cache-01/disk.vmdk',NULL,102400,true,'SCSI 0','Hard disk 1')
	`)

	// vdatastore
	exec(`INSERT INTO vdatastore VALUES ('DS1','obj-ds-001'),('DS2','obj-ds-002')`)

	// vnetwork — vm-003 and vm-008 have two NICs each
	exec(`INSERT INTO vnetwork VALUES
		('vm-001','VM Network','00:50:56:01:01:01','vmxnet3'),
		('vm-002','VM Network','00:50:56:01:01:02','vmxnet3'),
		('vm-003','VM Network','00:50:56:01:01:03','vmxnet3'),
		('vm-003','Backup Network','00:50:56:01:02:03','vmxnet3'),
		('vm-004','VM Network','00:50:56:02:01:01','vmxnet3'),
		('vm-005','VM Network','00:50:56:02:01:02','vmxnet3'),
		('vm-006','VM Network','00:50:56:01:01:06','vmxnet3'),
		('vm-007','VM Network','00:50:56:01:01:07','e1000e'),
		('vm-008','VM Network','00:50:56:02:01:08','vmxnet3'),
		('vm-008','Management Network','00:50:56:02:02:08','vmxnet3'),
		('vm-009','Legacy Network','00:50:56:01:01:09','e1000'),
		('vm-010','VM Network','00:50:56:01:01:10','vmxnet3')
	`)

	// concerns — vm-003 has 1 critical, vm-009 has 2 (warning + information)
	exec(`INSERT INTO concerns VALUES
		('vm-003','concern-001','RDM detected','Critical','Storage'),
		('vm-009','concern-002','Unsupported OS version','Warning','OS'),
		('vm-009','concern-003','Low resources','Information','Resources')
	`)
}
