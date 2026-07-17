package v2_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	v2 "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

var _ = Describe("ApplicationService", func() {
	var (
		ctx    context.Context
		pool   *store.Pool
		db     *store.Database
		st     *store.Store2
		sqlDB  *sql.DB
		srv    *v2.ApplicationService
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "application-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)

		dbPath := filepath.Join(tmpDir, "collection.duckdb")
		db, err = pool.NewDatabase("collection", dbPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())

		Expect(db.Migrate(ctx, func(ctx context.Context, d *sql.DB) error {
			sqlDB = d
			s, err := db.Store()
			if err != nil {
				return err
			}
			if err := duckdb_parser.New(s.Querier(), nil).Init(); err != nil {
				return err
			}
			return migrations.RunCollection(ctx, d, "collection")
		})).To(Succeed())

		st, err = db.Store()
		Expect(err).NotTo(HaveOccurred())

		srv, err = v2.NewApplicationService(st)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		pool.Close()
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	Context("NewApplicationService", func() {
		It("should load embedded application definitions", func() {
			Expect(srv).NotTo(BeNil())
		})
	})

	Context("List", func() {
		It("should return empty when no applications matched", func() {
			apps, err := srv.List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(apps).To(BeEmpty())
		})

		It("should return matched applications after MatchApplications", func() {
			Expect(insertVMWithGuestApps(ctx, sqlDB, "vm-1", "web-server", []string{"httpd", "sshd"})).To(Succeed())
			Expect(insertVMWithGuestApps(ctx, sqlDB, "vm-2", "db-server", []string{"postgres", "sshd"})).To(Succeed())

			Expect(srv.MatchApplications(ctx)).To(Succeed())

			apps, err := srv.List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(apps)).To(BeNumerically(">=", 1))

			var names []string
			for _, a := range apps {
				names = append(names, a.Name)
			}
			Expect(names).To(ContainElement("Apache HTTP Server"))
			Expect(names).To(ContainElement("PostgreSQL"))
		})
	})

	Context("MatchApplications", func() {
		It("should match VMs with single-process apps", func() {
			Expect(insertVMWithGuestApps(ctx, sqlDB, "vm-1", "web-01", []string{"httpd"})).To(Succeed())

			Expect(srv.MatchApplications(ctx)).To(Succeed())

			apps, err := srv.List(ctx)
			Expect(err).NotTo(HaveOccurred())

			var apache *struct{ vmCount int }
			for _, a := range apps {
				if a.Name == "Apache HTTP Server" {
					apache = &struct{ vmCount int }{vmCount: a.VMCount}
					break
				}
			}
			Expect(apache).NotTo(BeNil())
			Expect(apache.vmCount).To(Equal(1))
		})

		It("should require min_matched processes for multi-process apps", func() {
			// Automic Automation Engine requires min_matched=2 (ucsrvwp, ucsrvcp)
			Expect(insertVMWithGuestApps(ctx, sqlDB, "vm-1", "auto-01", []string{"ucsrvwp"})).To(Succeed())

			Expect(srv.MatchApplications(ctx)).To(Succeed())

			apps, err := srv.List(ctx)
			Expect(err).NotTo(HaveOccurred())

			for _, a := range apps {
				Expect(a.Name).NotTo(Equal("Automic Automation Engine"))
			}
		})

		It("should match when min_matched threshold is met", func() {
			Expect(insertVMWithGuestApps(ctx, sqlDB, "vm-1", "auto-01", []string{"ucsrvwp", "ucsrvcp"})).To(Succeed())

			Expect(srv.MatchApplications(ctx)).To(Succeed())

			apps, err := srv.List(ctx)
			Expect(err).NotTo(HaveOccurred())

			var found bool
			for _, a := range apps {
				if a.Name == "Automic Automation Engine" {
					found = true
					break
				}
			}
			Expect(found).To(BeTrue())
		})

		It("should match multiple VMs to the same application", func() {
			Expect(insertVMWithGuestApps(ctx, sqlDB, "vm-1", "web-01", []string{"httpd"})).To(Succeed())
			Expect(insertVMWithGuestApps(ctx, sqlDB, "vm-2", "web-02", []string{"httpd", "nginx"})).To(Succeed())
			Expect(insertVMWithGuestApps(ctx, sqlDB, "vm-3", "app-01", []string{"nginx"})).To(Succeed())

			Expect(srv.MatchApplications(ctx)).To(Succeed())

			apps, err := srv.List(ctx)
			Expect(err).NotTo(HaveOccurred())

			for _, a := range apps {
				if a.Name == "Apache HTTP Server" {
					Expect(a.VMCount).To(Equal(2))
					Expect(a.VMs).To(HaveLen(2))
					return
				}
			}
			Fail("Apache HTTP Server not found")
		})

		It("should return results sorted alphabetically", func() {
			Expect(insertVMWithGuestApps(ctx, sqlDB, "vm-1", "multi-01", []string{"httpd", "postgres", "nginx"})).To(Succeed())

			Expect(srv.MatchApplications(ctx)).To(Succeed())

			apps, err := srv.List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(apps)).To(BeNumerically(">=", 2))

			for i := 1; i < len(apps); i++ {
				Expect(apps[i].Name >= apps[i-1].Name).To(BeTrue(),
					"expected %s >= %s", apps[i].Name, apps[i-1].Name)
			}
		})

		It("should replace previous matches on re-run", func() {
			Expect(insertVMWithGuestApps(ctx, sqlDB, "vm-1", "web-01", []string{"httpd"})).To(Succeed())
			Expect(srv.MatchApplications(ctx)).To(Succeed())

			apps1, err := srv.List(ctx)
			Expect(err).NotTo(HaveOccurred())

			// Remove the VM's guest apps
			_, err = sqlDB.ExecContext(ctx, `UPDATE vinfo SET guest_apps = '[]' WHERE "VM ID" = 'vm-1'`)
			Expect(err).NotTo(HaveOccurred())

			Expect(srv.MatchApplications(ctx)).To(Succeed())

			apps2, err := srv.List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(apps2)).To(BeNumerically("<", len(apps1)))
		})

		It("should handle no VMs", func() {
			Expect(srv.MatchApplications(ctx)).To(Succeed())

			apps, err := srv.List(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(apps).To(BeEmpty())
		})
	})
})

func insertVMWithGuestApps(ctx context.Context, db *sql.DB, vmID, vmName string, apps []string) error {
	appsJSON, err := json.Marshal(toGuestAppsJSON(apps))
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO vinfo ("VM ID", "VM", "Powerstate", "Connection state", "Cluster", "Datacenter",
			"Host", "Folder ID", "Firmware", "SMBIOS UUID", "Memory", "CPUs",
			"OS according to the configuration file", "DNS Name", "Primary IP Address",
			"In Use MiB", "Template", "FT State", "guest_apps")
		VALUES (?, ?, 'poweredOn', 'connected', 'cluster-1', 'dc-1',
			'host-1', 'folder-1', 'bios', 'uuid-1', 4096, 2,
			'CentOS 7', '', '', 0, false, 'notConfigured', ?)
	`, vmID, vmName, string(appsJSON))
	return err
}

func toGuestAppsJSON(names []string) []map[string]string {
	result := make([]map[string]string, len(names))
	for i, n := range names {
		result[i] = map[string]string{"name": n}
	}
	return result
}
