package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/xuri/excelize/v2"

	v2 "github.com/kubev2v/assisted-migration-agent/agent/api/v2"
	v1 "github.com/kubev2v/assisted-migration-agent/analyzer/api/v1"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	vsphere "github.com/kubev2v/forklift/pkg/controller/provider/model/vsphere"
	libmodel "github.com/kubev2v/forklift/pkg/lib/inventory/model"
	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"
)

var _ = Describe("VM host physical NICs", func() {
	var (
		ctx    context.Context
		dir    string
		db     *store.Database
		st     *store.Store
		parser *duckdb_parser.Parser
	)
	BeforeEach(func() {
		ctx = context.Background()
		dir = GinkgoT().TempDir()
		pool := store.NewPool(time.Minute)
		DeferCleanup(pool.Close)
		var err error
		db, err = pool.NewDatabase("coll", filepath.Join(dir, "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		pool.Add(db)
		st, err = db.Store()
		Expect(err).NotTo(HaveOccurred())
		parser = duckdb_parser.New(st.Querier(), nil)
		// Both collection pipelines initialize parser tables before agent tables.
		Expect(db.Migrate(ctx, func(ctx context.Context, raw *sql.DB) error {
			if err := parser.Init(); err != nil {
				return err
			}
			return migrations.RunCollection(ctx, raw, "collection")
		})).To(Succeed())
	})
	It("preserves host ownership and nullable speeds in both APIs and CSV/XLSX exports", func() {
		_, err := st.Querier().ExecContext(ctx, `INSERT INTO vinfo ("VM ID", "VM", "Host") VALUES
     ('vm-1','first','host-1'), ('vm-2','second','host-1'), ('vm-3','third','host-2'),
     ('vm-4','unknown',''), ('vm-5','missing',NULL);
     INSERT INTO vnetwork ("VM ID", "NIC label", "Network", "Mac Address") VALUES ('vm-1','adapter','network','00:11:22:33:44:55')`)
		Expect(err).NotTo(HaveOccurred())
		vm, err := st.VM().Get(ctx, "vm-1")
		Expect(err).NotTo(HaveOccurred())
		Expect(vm.HostPhysicalNICs).To(BeEmpty())
		Expect(st.Export().CopyScope(ctx, "vms", filepath.Join(dir, "empty.csv"))).To(Succeed())
		Expect(db.Migrate(ctx, func(ctx context.Context, raw *sql.DB) error { return migrations.RunCollection(ctx, raw, "collection") })).To(Succeed())
		_, err = st.Querier().ExecContext(ctx, `INSERT INTO vnic VALUES
     ('host-1','vmnic1',NULL), ('host-1','vmnic0',10000),
     ('host-2','vmnic0',25000), ('','unowned',1000), (NULL,'unowned-null',1000)`)
		Expect(err).NotTo(HaveOccurred())
		want := map[string]string{
			"vm-1": `[{"id":"vmnic0","linkSpeedMbps":10000},{"id":"vmnic1"}]`,
			"vm-2": `[{"id":"vmnic0","linkSpeedMbps":10000},{"id":"vmnic1"}]`,
			"vm-3": `[{"id":"vmnic0","linkSpeedMbps":25000}]`,
			"vm-4": `[]`, "vm-5": `[]`,
		}
		for id, expected := range want {
			vm, err := st.VM().Get(ctx, id)
			Expect(err).NotTo(HaveOccurred())
			detail := v2.NewVirtualMachineDetailFromModel(*vm)
			encoded, err := json.Marshal(detail)
			Expect(err).NotTo(HaveOccurred())
			var response map[string]json.RawMessage
			Expect(json.Unmarshal(encoded, &response)).To(Succeed())
			Expect(string(response["hostPhysicalNics"])).To(MatchJSON(expected))
			analyzerDetail := v1.NewVirtualMachineDetailFromModel(*vm)
			analyzerJSON, err := json.Marshal(analyzerDetail)
			Expect(err).NotTo(HaveOccurred())
			var analyzerResponse map[string]json.RawMessage
			Expect(json.Unmarshal(analyzerJSON, &analyzerResponse)).To(Succeed())
			Expect(string(analyzerResponse["hostPhysicalNics"])).To(MatchJSON(expected))
			if id == "vm-1" {
				Expect(vm.NICs).To(HaveLen(1))
				Expect(vm.NICs[0].MAC).To(Equal("00:11:22:33:44:55"))
				Expect(vm.HostPhysicalNICs[1].LinkSpeedMbps).To(BeNil())
			}
		}
		checkRows := func(rows [][]string) {
			Expect(rows).To(HaveLen(len(want) + 1))
			idCol := slices.Index(rows[0], "id")
			nicCol := slices.Index(rows[0], "host_physical_nics")
			Expect(idCol).To(BeNumerically(">=", 0))
			Expect(nicCol).To(Equal(len(rows[0]) - 1))
			Expect(rows[0]).To(ContainElement("network_1"))
			seen := map[string]bool{}
			for _, row := range rows[1:] {
				Expect(seen[row[idCol]]).To(BeFalse())
				seen[row[idCol]] = true
				Expect(row[nicCol]).To(MatchJSON(want[row[idCol]]))
			}
		}
		path := filepath.Join(dir, "vms.csv")
		Expect(st.Export().CopyScope(ctx, "vms", path)).To(Succeed())
		data, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
		Expect(err).NotTo(HaveOccurred())
		checkRows(rows)
		var output bytes.Buffer
		Expect(services.NewExportService(st).WriteExcel(ctx, []string{"vms"}, &output)).To(Succeed())
		workbook, err := excelize.OpenReader(bytes.NewReader(output.Bytes()))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(workbook.Close)
		rows, err = workbook.GetRows("VMs")
		Expect(err).NotTo(HaveOccurred())
		checkRows(rows)
	})
	DescribeTable("imports fresh source data through both APIs and VM exports", func(source string) {
		var count int
		Expect(st.Querier().QueryRowContext(ctx, `SELECT COUNT(*) FROM vnic`).Scan(&count)).To(Succeed())
		Expect(count).To(BeZero())
		var result duckdb_parser.ValidationResult
		var err error
		if source == "rvtools" {
			path := filepath.Join(dir, "source.xlsx")
			workbook := excelize.NewFile()
			DeferCleanup(workbook.Close)
			for sheet, rows := range map[string][][]interface{}{
				"vInfo": {
					{"VM ID", "VM", "Host", "Cluster", "VI SDK UUID", "CPUs", "Memory", "Template"},
					{"vm-1", "first", "host-1", "cluster", "vc", 2, 4096, "false"},
					{"vm-2", "second", "host-1", "cluster", "vc", 2, 4096, "false"},
					{"vm-3", "third", "host-2", "cluster", "vc", 2, 4096, "false"},
				},
				"vNIC": {
					{"Host", "Network Device", "Speed"},
					{"host-1", "vmnic1", 0}, {"host-1", "vmnic0", 10000}, {"host-2", "vmnic0", 25000},
				},
			} {
				_, err := workbook.NewSheet(sheet)
				Expect(err).NotTo(HaveOccurred())
				for i, row := range rows {
					cell, err := excelize.CoordinatesToCellName(1, i+1)
					Expect(err).NotTo(HaveOccurred())
					Expect(workbook.SetSheetRow(sheet, cell, &row)).To(Succeed())
				}
			}
			Expect(workbook.SaveAs(path)).To(Succeed())
			result, err = parser.IngestRvTools(ctx, path)
		} else {
			path := filepath.Join(dir, "source.db")
			createHostPhysicalNICForkliftSource(path)
			result, err = parser.IngestSqlite(ctx, path)
		}
		Expect(err).NotTo(HaveOccurred())
		Expect(result.IsValid()).To(BeTrue(), "%v", result.Errors)
		Expect(st.Querier().QueryRowContext(ctx, `SELECT COUNT(*) FROM vnic`).Scan(&count)).To(Succeed())
		Expect(count).To(Equal(3))
		want := map[string]string{
			"vm-1": `[{"id":"vmnic0","linkSpeedMbps":10000},{"id":"vmnic1"}]`,
			"vm-2": `[{"id":"vmnic0","linkSpeedMbps":10000},{"id":"vmnic1"}]`,
			"vm-3": `[{"id":"vmnic0","linkSpeedMbps":25000}]`,
		}
		for id, expected := range want {
			vm, err := st.VM().Get(ctx, id)
			Expect(err).NotTo(HaveOccurred())
			for _, detail := range []interface{}{v2.NewVirtualMachineDetailFromModel(*vm), v1.NewVirtualMachineDetailFromModel(*vm)} {
				encoded, err := json.Marshal(detail)
				Expect(err).NotTo(HaveOccurred())
				var response map[string]json.RawMessage
				Expect(json.Unmarshal(encoded, &response)).To(Succeed())
				Expect(string(response["hostPhysicalNics"])).To(MatchJSON(expected))
			}
		}
		csvPath := filepath.Join(dir, "vms.csv")
		Expect(st.Export().CopyScope(ctx, "vms", csvPath)).To(Succeed())
		output, err := os.Open(csvPath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(output.Close)
		rows, err := csv.NewReader(output).ReadAll()
		Expect(err).NotTo(HaveOccurred())
		Expect(rows).To(HaveLen(4))
		idColumn := slices.Index(rows[0], "id")
		nicColumn := slices.Index(rows[0], "host_physical_nics")
		Expect(idColumn).To(BeNumerically(">=", 0))
		Expect(nicColumn).To(BeNumerically(">=", 0))
		for _, row := range rows[1:] {
			Expect(row[nicColumn]).To(MatchJSON(want[row[idColumn]]))
		}
	}, Entry("RVTools", "rvtools"), Entry("Forklift", "forklift"))
})

func createHostPhysicalNICForkliftSource(path string) {
	GinkgoHelper()
	db := libmodel.New(path, &vsphere.About{}, &vsphere.Cluster{}, &vsphere.Host{}, &vsphere.VM{}, &vsphere.Folder{}, &vsphere.Datacenter{}, &vsphere.Network{}, &vsphere.Datastore{})
	Expect(db.Open(true)).To(Succeed())
	defer func() { Expect(db.Close(false)).To(Succeed()) }()
	for _, model := range []libmodel.Model{
		&vsphere.About{Base: vsphere.Base{ID: "about"}, APIVersion: "8.0", InstanceUuid: "vc"},
		&vsphere.Cluster{Base: vsphere.Base{ID: "cluster", Name: "cluster"}},
		&vsphere.Host{Base: vsphere.Base{ID: "host-1"}, Cluster: "cluster", Network: vsphere.HostNetwork{PNICs: []vsphere.PNIC{{Key: "vmnic1"}, {Key: "vmnic0", LinkSpeed: 10000}}}},
		&vsphere.Host{Base: vsphere.Base{ID: "host-2"}, Cluster: "cluster", Network: vsphere.HostNetwork{PNICs: []vsphere.PNIC{{Key: "vmnic0", LinkSpeed: 25000}}}},
		&vsphere.VM{Base: vsphere.Base{ID: "vm-1", Name: "first"}, Host: "host-1", CpuCount: 2, MemoryMB: 4096},
		&vsphere.VM{Base: vsphere.Base{ID: "vm-2", Name: "second"}, Host: "host-1", CpuCount: 2, MemoryMB: 4096},
		&vsphere.VM{Base: vsphere.Base{ID: "vm-3", Name: "third"}, Host: "host-2", CpuCount: 2, MemoryMB: 4096},
	} {
		Expect(db.Insert(model)).To(Succeed())
	}
}
