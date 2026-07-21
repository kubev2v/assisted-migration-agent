package v2_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	v2 "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("VddkService", func() {
	var (
		dataDir  string
		srv      *v2.VddkService
		pool     *store.Pool
		database *store.Database
	)

	BeforeEach(func() {
		ctx := context.Background()

		var err error
		dataDir, err = os.MkdirTemp("", "vddk-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(0)
		mainPath := filepath.Join(dataDir, "agent.duckdb")
		database, err = pool.NewDatabase(store.MainDatabaseID, mainPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(database.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
			s, err := database.Store()
			if err != nil {
				return err
			}
			parser := duckdb_parser.New(s.Querier(), nil)
			if err := parser.Init(); err != nil {
				return err
			}
			return migrations.RunMain(ctx, db)
		})).To(Succeed())
		pool.Add(database)

		collPath := filepath.Join(dataDir, "default_collection.duckdb")
		collDB, err := pool.NewDatabase("default-coll", collPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(collDB.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
			s, err := collDB.Store()
			if err != nil {
				return err
			}
			parser := duckdb_parser.New(s.Querier(), nil)
			if err := parser.Init(); err != nil {
				return err
			}
			return migrations.RunCollection(ctx, db, "default_collection")
		})).To(Succeed())

		collSt, err := collDB.Store()
		Expect(err).NotTo(HaveOccurred())
		_, err = collSt.Querier().ExecContext(ctx,
			`INSERT INTO about ("APIVersion", "Product", "InstanceUuid") VALUES (?, ?, ?)`,
			"8.0.3", "VMware vCenter Server", uuid.New().String())
		Expect(err).NotTo(HaveOccurred())
		pool.Add(collDB)

		srv = v2.NewVddkService(dataDir, pool)
	})

	AfterEach(func() {
		pool.Close()
		if dataDir != "" {
			_ = os.RemoveAll(dataDir)
		}
	})

	Describe("Upload", func() {
		It("extracts tar.gz, saves status and returns version/md5", func() {
			tarGz := test.BuildTarGz(
				test.TarEntry{
					Path:    "lib/lib64.so",
					Content: "vddk-library-content",
				})
			filename := "VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz"
			status, err := srv.Upload(context.Background(), filename, bytes.NewReader(tarGz))
			Expect(err).NotTo(HaveOccurred())
			Expect(status).NotTo(BeNil())
			Expect(status.Version).To(Equal("8.0.3"))
			Expect(status.Md5).To(HaveLen(32))

			extracted := filepath.Join(dataDir, "vddk", "lib", "lib64.so")
			data, err := os.ReadFile(extracted)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal("vddk-library-content"))

			s, err := srv.Status(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Version).To(Equal(status.Version))
			Expect(s.Md5).To(Equal(status.Md5))
		})

		It("returns error when file is not a valid tar.gz", func() {
			status, err := srv.Upload(context.Background(), "VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", bytes.NewReader([]byte("not a tar.gz")))
			Expect(err).To(HaveOccurred())
			Expect(status).To(BeNil())
		})

		It("returns VddkUploadInProgressError when upload is already in progress", func() {
			tarGz := test.BuildTarGz(test.TarEntry{Path: "slow", Content: "x"})
			const concurrency = 4
			r := make([]io.Reader, concurrency)
			for i := 0; i < concurrency; i++ {
				r[i] = bytes.NewReader(tarGz)
			}

			var wg sync.WaitGroup
			results := make([]error, concurrency)
			for i := 0; i < concurrency; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					_, results[idx] = srv.Upload(context.Background(),
						"VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", r[idx])
				}(i)
			}
			wg.Wait()

			var successCount, inProgressCount int
			for _, err := range results {
				if err == nil {
					successCount++
				} else if srvErrors.IsOperationInProgressError(err) {
					inProgressCount++
				}
			}
			Expect(successCount).To(Equal(1))
			Expect(inProgressCount).To(Equal(concurrency - 1))
		})

		It("does not override previous content when upload is invalid", func() {
			tarGz := test.BuildTarGz(test.TarEntry{Path: "lib/lib64.so", Content: "original-vddk-content"})
			firstStatus, err := srv.Upload(context.Background(), "VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", bytes.NewReader(tarGz))
			Expect(err).NotTo(HaveOccurred())
			Expect(firstStatus).NotTo(BeNil())

			extractedPath := filepath.Join(dataDir, "vddk", "lib", "lib64.so")
			Expect(extractedPath).To(BeARegularFile())
			content, err := os.ReadFile(extractedPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("original-vddk-content"))

			_, err = srv.Upload(context.Background(), "VMware-vix-disklib-9.0.0-bad.x86_64.tar.gz", bytes.NewReader([]byte("not a tar.gz")))
			Expect(err).To(HaveOccurred())

			Expect(extractedPath).To(BeARegularFile())
			contentAfter, err := os.ReadFile(extractedPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(contentAfter)).To(Equal("original-vddk-content"))

			s, err := srv.Status(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Version).To(Equal(firstStatus.Version))
			Expect(s.Md5).To(Equal(firstStatus.Md5))
		})

		It("returns error when filename format is invalid", func() {
			tarGz := test.BuildTarGz(test.TarEntry{Path: "lib/foo.so", Content: "x"})
			_, err := srv.Upload(context.Background(), "invalid-name.tar.gz", bytes.NewReader(tarGz))
			Expect(err).To(HaveOccurred())
		})

		It("extracts symlinks from tar.gz", func() {
			tarGz := test.BuildTarGz(
				test.TarEntry{Path: "vmware-vix-disklib-distrib/lib64/libcares.so.2", Content: "so-payload"},
				test.TarEntry{Path: "vmware-vix-disklib-distrib/lib64/libcares.so", LinkTarget: "libcares.so.2"},
			)
			_, err := srv.Upload(context.Background(), "VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", bytes.NewReader(tarGz))
			Expect(err).NotTo(HaveOccurred())

			link := filepath.Join(dataDir, "vddk", "vmware-vix-disklib-distrib", "lib64", "libcares.so")
			fi, err := os.Lstat(link)
			Expect(err).NotTo(HaveOccurred())
			Expect(fi.Mode() & os.ModeSymlink).NotTo(BeZero())
			target, err := os.Readlink(link)
			Expect(err).NotTo(HaveOccurred())
			Expect(target).To(Equal("libcares.so.2"))
		})
	})

	Describe("Status", func() {
		It("returns VddkNotFoundError when no config exists", func() {
			_, err := srv.Status(context.Background())
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("returns saved status when config exists", func() {
			tarGz := test.BuildTarGz(test.TarEntry{Path: "lib/x.so", Content: "y"})
			uploaded, err := srv.Upload(context.Background(), "VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", bytes.NewReader(tarGz))
			Expect(err).NotTo(HaveOccurred())

			s, err := srv.Status(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(s.Version).To(Equal(uploaded.Version))
			Expect(s.Md5).To(Equal(uploaded.Md5))
		})
	})

	Describe("Security: Path Traversal Prevention", func() {
		It("blocks chained symlink attack", func() {
			tarGz := test.BuildTarGz(
				test.TarEntry{Path: "a/x", LinkTarget: ".."},
				test.TarEntry{Path: "a/x/evil.sh", Content: "malicious payload"},
			)
			_, err := srv.Upload(context.Background(), "VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", bytes.NewReader(tarGz))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("symlink escape detected"))
		})

		It("blocks absolute symlink escape", func() {
			tarGz := test.BuildTarGz(test.TarEntry{Path: "malicious", LinkTarget: "/etc/passwd"})
			_, err := srv.Upload(context.Background(), "VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", bytes.NewReader(tarGz))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("illegal symlink target"))
		})

		It("blocks directory traversal", func() {
			tarGz := test.BuildTarGz(test.TarEntry{Path: "../../etc/shadow", Content: "malicious"})
			_, err := srv.Upload(context.Background(), "VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", bytes.NewReader(tarGz))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("illegal file path"))
		})

		It("blocks relative symlink pointing outside destDir", func() {
			tarGz := test.BuildTarGz(test.TarEntry{Path: "a/b/c", LinkTarget: "../../../etc/passwd"})
			_, err := srv.Upload(context.Background(), "VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", bytes.NewReader(tarGz))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("illegal symlink target"))
		})

		It("allows legitimate VDDK internal symlinks", func() {
			tarGz := test.BuildTarGz(
				test.TarEntry{Path: "vmware-vix-disklib-distrib/lib64/libvixDiskLib.so.8.0.3", Content: "library-content"},
				test.TarEntry{Path: "vmware-vix-disklib-distrib/lib64/libvixDiskLib.so", LinkTarget: "libvixDiskLib.so.8.0.3"},
			)
			_, err := srv.Upload(context.Background(), "VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", bytes.NewReader(tarGz))
			Expect(err).NotTo(HaveOccurred())

			link := filepath.Join(dataDir, "vddk", "vmware-vix-disklib-distrib", "lib64", "libvixDiskLib.so")
			target, err := os.Readlink(link)
			Expect(err).NotTo(HaveOccurred())
			Expect(target).To(Equal("libvixDiskLib.so.8.0.3"))
		})
	})

	Describe("Version validation", func() {
		addCollectionWithAPIVersion := func(apiVersion string) {
			ctx := context.Background()
			collPath := filepath.Join(dataDir, "collection.duckdb")
			collDB, err := pool.NewDatabase("coll", collPath, time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			Expect(collDB.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
				s, err := collDB.Store()
				if err != nil {
					return err
				}
				parser := duckdb_parser.New(s.Querier(), nil)
				if err := parser.Init(); err != nil {
					return err
				}
				return migrations.RunCollection(ctx, db, "collection")
			})).To(Succeed())

			collSt, err := collDB.Store()
			Expect(err).NotTo(HaveOccurred())
			_, err = collSt.Querier().ExecContext(ctx,
				`INSERT INTO about ("APIVersion", "Product", "InstanceUuid") VALUES (?, ?, ?)`,
				apiVersion, "VMware vCenter Server", uuid.New().String())
			Expect(err).NotTo(HaveOccurred())

			pool.Add(collDB)
		}

		It("returns InvalidVersionError when VDDK version does not match vCenter API version", func() {
			addCollectionWithAPIVersion("8.0.3")

			tarGz := test.BuildTarGz(test.TarEntry{Path: "lib/x.so", Content: "y"})
			_, err := srv.Upload(context.Background(),
				"VMware-vix-disklib-9.0.0-23950268.x86_64.tar.gz", bytes.NewReader(tarGz))
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsInvalidVersionError(err)).To(BeTrue())
			var inv *srvErrors.InvalidVersionError
			Expect(errors.As(err, &inv)).To(BeTrue())
			Expect(inv.Expected).To(Equal("8.0"))
			Expect(inv.Actual).To(Equal("9.0.0"))
		})

		It("succeeds when vCenter API version has more than three components", func() {
			addCollectionWithAPIVersion("8.0.3.12345")

			tarGz := test.BuildTarGz(test.TarEntry{Path: "lib/x.so", Content: "y"})
			status, err := srv.Upload(context.Background(),
				"VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", bytes.NewReader(tarGz))
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Version).To(Equal("8.0.3"))
		})
	})

	Describe("extractVersion", func() {
		It("parses version from VMware-vix-disklib-X.Y.Z-... filename", func() {
			tarGz := test.BuildTarGz(test.TarEntry{Path: "lib/x.so", Content: "z"})
			status, err := srv.Upload(context.Background(), "VMware-vix-disklib-8.0.1-12345678.x86_64.tar.gz", bytes.NewReader(tarGz))
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Version).To(Equal("8.0.1"))
		})

		It("extracts version from lib64 libvixDiskLib.so when filename has no version", func() {
			tarGz := test.BuildTarGz(test.TarEntry{
				Path:    "vmware-vix-disklib-distrib/lib64/libvixDiskLib.so.8.0.3",
				Content: "library-content",
			})
			status, err := srv.Upload(context.Background(), "vddk.tar.gz", bytes.NewReader(tarGz))
			Expect(err).NotTo(HaveOccurred())
			Expect(status).NotTo(BeNil())
			Expect(status.Version).To(Equal("8.0.3"))
		})

		It("returns error when filename has no version and tar has no lib64 libvixDiskLib.so", func() {
			tarGz := test.BuildTarGz(test.TarEntry{Path: "lib/foo.so", Content: "x"})
			_, err := srv.Upload(context.Background(), "vddk.tar.gz", bytes.NewReader(tarGz))
			Expect(err).To(HaveOccurred())
		})
	})
})
