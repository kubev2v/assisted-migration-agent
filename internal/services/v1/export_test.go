package v1_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"io"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("ExportService", func() {
	var (
		ctx    context.Context
		db     *sql.DB
		svc    *services.ExportService
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "export-test-*")
		Expect(err).NotTo(HaveOccurred())

		db, err = store.NewConnection(nil, filepath.Join(tmpDir, "agent.duckdb"))
		Expect(err).NotTo(HaveOccurred())

		st := store.NewStore(db, test.NewMockValidator())
		Expect(st.Migrate(ctx, "")).To(Succeed())
		Expect(st.InitCollection(ctx)).To(Succeed())
		Expect(test.InsertVMs(ctx, db)).To(Succeed())

		svc = services.NewExportService(st)
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	Context("WriteZip scope files", func() {
		DescribeTable("should produce expected files for scope",
			func(scope string, files []string) {
				zipData := exportZip(ctx, svc, []string{scope})
				entries := readZipEntries(zipData)
				for _, name := range files {
					Expect(entries).To(HaveKey(name))
					Expect(entries[name]).NotTo(BeEmpty())
				}
			},
			Entry("overview", "overview", []string{"overview.csv"}),
			Entry("hosts", "hosts", []string{"hosts.csv"}),
			Entry("clusters", "clusters", []string{"clusters.csv"}),
			Entry("datastores", "datastores", []string{"datastores.csv"}),
			Entry("vms", "vms", []string{"vms.csv"}),
			Entry("network", "network", []string{"networks.csv"}),
			Entry("utilization", "utilization", []string{"vm_utilization.csv", "cluster_utilization.csv"}),
			Entry("applications", "applications", []string{"applications.csv"}),
			Entry("groups", "groups", []string{"groups.csv"}),
			Entry("inspection", "inspection", []string{"inspection.csv"}),
			Entry("storage-forecast", "storage-forecast", []string{"storage-forecast.csv"}),
		)
	})

	Context("WriteZip all scopes", func() {
		It("should produce all expected files", func() {
			allScopes := []string{
				"overview", "hosts", "clusters", "datastores", "vms", "network",
				"utilization", "applications", "groups", "inspection", "storage-forecast",
			}
			wantFiles := []string{
				"overview.csv", "hosts.csv", "clusters.csv", "datastores.csv", "vms.csv",
				"networks.csv", "vm_utilization.csv", "cluster_utilization.csv",
				"applications.csv", "groups.csv", "inspection.csv", "storage-forecast.csv",
			}

			zipData := exportZip(ctx, svc, allScopes)
			entries := readZipEntries(zipData)
			Expect(entries).To(HaveLen(len(wantFiles)))
			for _, name := range wantFiles {
				Expect(entries).To(HaveKey(name))
			}
		})
	})

	Context("WriteZip valid archive", func() {
		It("should produce valid ZIP magic bytes", func() {
			data := exportZip(ctx, svc, []string{"overview"})
			Expect(len(data)).To(BeNumerically(">", 2))
			Expect(data[0]).To(Equal(byte(0x50)))
			Expect(data[1]).To(Equal(byte(0x4b)))
		})
	})

	Context("WriteZip errors", func() {
		It("should fail with cancelled context", func() {
			cancelled, cancel := context.WithCancel(ctx)
			cancel()

			err := svc.WriteZip(cancelled, []string{"overview"}, &bytes.Buffer{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(context.Canceled.Error()))
		})
	})

	Context("WriteZip overview", func() {
		It("should have correct row count", func() {
			entries := readZipEntries(exportZip(ctx, svc, []string{"overview"}))
			count := csvRowCount(entries["overview.csv"])
			Expect(count).To(Equal(len(test.VMs)))
		})

		DescribeTable("should have correct migration_status",
			func(vmID, status string) {
				entries := readZipEntries(exportZip(ctx, svc, []string{"overview"}))
				rows := csvRowsByCol(entries["overview.csv"], "id")
				Expect(rows).To(HaveKey(vmID))
				Expect(rows[vmID]["migration_status"]).To(Equal(status))
			},
			Entry("vm-001 Ready", "vm-001", "Ready"),
			Entry("vm-003 Review", "vm-003", "Review"),
			Entry("vm-007 Blocked", "vm-007", "Blocked"),
		)
	})

	Context("WriteZip CSV injection", func() {
		It("should escape formula-like values", func() {
			_, err := db.ExecContext(ctx, `UPDATE vinfo SET "VM" = '=1+1' WHERE "VM ID" = 'vm-001'`)
			Expect(err).NotTo(HaveOccurred())

			entries := readZipEntries(exportZip(ctx, svc, []string{"overview"}))
			rows := csvRowsByCol(entries["overview.csv"], "id")
			Expect(rows).To(HaveKey("vm-001"))
			Expect(rows["vm-001"]["name"]).To(Equal("'=1+1"))
		})
	})

	Context("WriteZip network", func() {
		It("should have correct NIC row count", func() {
			entries := readZipEntries(exportZip(ctx, svc, []string{"network"}))
			count := csvRowCount(entries["networks.csv"])
			Expect(count).To(Equal(len(test.NICs)))
		})
	})

	Context("WriteZip utilization", func() {
		It("should produce empty utilization without rightsizing report", func() {
			entries := readZipEntries(exportZip(ctx, svc, []string{"utilization"}))
			Expect(entries).To(HaveKey("vm_utilization.csv"))
			Expect(entries).To(HaveKey("cluster_utilization.csv"))
			Expect(csvRowCount(entries["vm_utilization.csv"])).To(Equal(0))
			Expect(string(entries["vm_utilization.csv"])).To(ContainSubstring("vm_name"))
		})

		It("should produce utilization rows with rightsizing report", func() {
			Expect(test.InsertVMUtilization(ctx, db)).To(Succeed())

			entries := readZipEntries(exportZip(ctx, svc, []string{"utilization"}))
			Expect(csvRowCount(entries["vm_utilization.csv"])).To(Equal(len(test.Utilizations)))
			Expect(csvRowCount(entries["cluster_utilization.csv"])).To(BeNumerically(">=", 1))
		})
	})
})

func exportZip(ctx context.Context, svc *services.ExportService, scopes []string) []byte {
	var buf bytes.Buffer
	ExpectWithOffset(1, svc.WriteZip(ctx, scopes, &buf)).To(Succeed())
	ExpectWithOffset(1, buf.Len()).To(BeNumerically(">", 0))
	return buf.Bytes()
}

func readZipEntries(data []byte) map[string][]byte {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	files := make(map[string][]byte, len(reader.File))
	for _, f := range reader.File {
		rc, err := f.Open()
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		ExpectWithOffset(1, err).NotTo(HaveOccurred())
		files[f.Name] = body
	}
	return files
}

func csvRowCount(data []byte) int {
	records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	if len(records) <= 1 {
		return 0
	}
	return len(records) - 1
}

func csvRowsByCol(data []byte, keyCol string) map[string]map[string]string {
	records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, records).NotTo(BeEmpty())

	header := records[0]
	keyIdx := -1
	for i, col := range header {
		if col == keyCol {
			keyIdx = i
			break
		}
	}
	ExpectWithOffset(1, keyIdx).To(BeNumerically(">=", 0))

	rows := make(map[string]map[string]string, len(records)-1)
	for _, record := range records[1:] {
		row := make(map[string]string, len(header))
		for i, col := range header {
			row[col] = record[i]
		}
		rows[record[keyIdx]] = row
	}
	return rows
}
