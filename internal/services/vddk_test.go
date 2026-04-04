package services_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("VddkService", func() {
	var (
		dataDir string
		srv     *services.VddkService
		st      *store.Store
		db      *sql.DB
	)

	BeforeEach(func() {
		var err error
		dataDir, err = os.MkdirTemp("", "vddk-test-*")
		Expect(err).NotTo(HaveOccurred())

		db, err = store.NewDB(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		err = migrations.Run(context.Background(), db)
		Expect(err).NotTo(HaveOccurred())
		st = store.NewStore(db, test.NewMockValidator())

		srv = services.NewVddkService(dataDir, st)
	})

	AfterEach(func() {
		if dataDir != "" {
			_ = os.RemoveAll(dataDir)
		}
		if db != nil {
			_ = db.Close()
		}
	})

	Describe("Upload", func() {
		It("extracts symlinks from tar.gz", func() {
			tarGz := test.BuildTarGz(
				test.TarEntry{
					Path:    "vmware-vix-disklib-distrib/lib64/libcares.so.2",
					Content: "so-payload",
				},
				test.TarEntry{
					Path:       "vmware-vix-disklib-distrib/lib64/libcares.so",
					LinkTarget: "libcares.so.2",
				},
			)
			filename := "VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz"
			_, err := srv.Upload(context.Background(), filename, bytes.NewReader(tarGz))
			Expect(err).NotTo(HaveOccurred())

			link := filepath.Join(dataDir, "vddk", "vmware-vix-disklib-distrib", "lib64", "libcares.so")
			fi, err := os.Lstat(link)
			Expect(err).NotTo(HaveOccurred())
			Expect(fi.Mode()&os.ModeSymlink != 0).To(BeTrue())
			target, err := os.Readlink(link)
			Expect(err).NotTo(HaveOccurred())
			Expect(target).To(Equal("libcares.so.2"))
		})

		It("extracts tar.gz, saves status and returns version/bytes/md5", func() {
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

			// Extracted content exists
			extracted := filepath.Join(dataDir, "vddk", "lib", "lib64.so")
			data, err := os.ReadFile(extracted)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal("vddk-library-content"))

			// Status is persisted
			st, err := srv.Status(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(st.Version).To(Equal(status.Version))
			Expect(st.Md5).To(Equal(status.Md5))
		})

		It("returns error when file is not a valid tar.gz", func() {
			invalidContent := []byte("not a tar.gz file")
			filename := "VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz"
			status, err := srv.Upload(context.Background(), filename, bytes.NewReader(invalidContent))
			Expect(err).To(HaveOccurred())
			Expect(status).To(BeNil())
		})

		It("does not override previous content when upload is invalid", func() {
			// Upload valid VDDK first
			tarGz := test.BuildTarGz(
				test.TarEntry{
					Path:    "lib/lib64.so",
					Content: "original-vddk-content",
				})
			filename := "VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz"
			firstStatus, err := srv.Upload(context.Background(), filename, bytes.NewReader(tarGz))

			Expect(err).NotTo(HaveOccurred())
			Expect(firstStatus).NotTo(BeNil())

			extractedPath := filepath.Join(dataDir, "vddk", "lib", "lib64.so")
			Expect(extractedPath).To(BeARegularFile())
			content, err := os.ReadFile(extractedPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("original-vddk-content"))

			// Attempt upload of bad file
			_, err = srv.Upload(context.Background(),
				"VMware-vix-disklib-9.0.0-bad.x86_64.tar.gz",
				bytes.NewReader([]byte("not a tar.gz")))
			Expect(err).To(HaveOccurred())

			// Previous extracted content must still be present and unchanged
			Expect(extractedPath).To(BeARegularFile())
			contentAfter, err := os.ReadFile(extractedPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(contentAfter)).To(Equal("original-vddk-content"))

			// Status must still reflect the first successful upload
			st, err := srv.Status(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(st.Version).To(Equal(firstStatus.Version))
			Expect(st.Md5).To(Equal(firstStatus.Md5))
		})

		It("returns error when filename format is invalid", func() {
			tarGz := test.BuildTarGz(
				test.TarEntry{
					Path:    "lib/foo.so",
					Content: "x",
				})
			_, err := srv.Upload(context.Background(), "invalid-name.tar.gz", bytes.NewReader(tarGz))
			Expect(err).To(HaveOccurred())
		})

		It("returns VddkUploadInProgressError when upload is already in progress", func() {
			tarGz := test.BuildTarGz(
				test.TarEntry{
					Path:    "slow",
					Content: "x",
				})
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
			Expect(successCount).To(Equal(1), "exactly one upload should succeed")
			Expect(inProgressCount).To(Equal(concurrency-1), "all other uploads should get in-progress error")
		})

		It("returns InvalidVersionError when VDDK version does not match vCenter API version from about", func() {
			_, err := db.ExecContext(context.Background(),
				`INSERT INTO about ("APIVersion", "Product", "InstanceUuid") VALUES (?, ?, ?)`,
				"8.0.3", "VMware vCenter Server", uuid.New())
			Expect(err).NotTo(HaveOccurred())

			tarGz := test.BuildTarGz(
				test.TarEntry{
					Path:    "lib/x.so",
					Content: "y",
				})
			_, err = srv.Upload(context.Background(),
				"VMware-vix-disklib-9.0.0-23950268.x86_64.tar.gz", bytes.NewReader(tarGz))
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsInvalidVersionError(err)).To(BeTrue())
			var inv *srvErrors.InvalidVersionError
			Expect(errors.As(err, &inv)).To(BeTrue())
			Expect(inv.Expected).To(Equal("8.0.3"))
			Expect(inv.Actual).To(Equal("9.0.0"))
		})

		It("succeeds when vCenter API version has more than three components (compares x.y.z only)", func() {
			_, err := db.ExecContext(context.Background(),
				`INSERT INTO about ("APIVersion", "Product", "InstanceUuid") VALUES (?, ?, ?)`,
				"8.0.3.12345", "VMware vCenter Server", "test-instance-uuid")
			Expect(err).NotTo(HaveOccurred())

			tarGz := test.BuildTarGz(
				test.TarEntry{
					Path:    "lib/x.so",
					Content: "y",
				})
			status, err := srv.Upload(context.Background(),
				"VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", bytes.NewReader(tarGz))
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Version).To(Equal("8.0.3"))
		})
	})

	Describe("Status", func() {
		It("returns VddkNotFoundError when no config exists", func() {
			_, err := srv.Status(context.Background())
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("returns saved status when config exists", func() {
			// Upload once to create config
			tarGz := test.BuildTarGz(
				test.TarEntry{
					Path:    "lib/x.so",
					Content: "y",
				})
			uploaded, err := srv.Upload(context.Background(),
				"VMware-vix-disklib-8.0.3-23950268.x86_64.tar.gz", bytes.NewReader(tarGz))
			Expect(err).NotTo(HaveOccurred())

			st, err := srv.Status(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(st.Version).To(Equal(uploaded.Version))
			Expect(st.Md5).To(Equal(uploaded.Md5))
		})
	})

	Describe("extractVersion", func() {
		// extractVersion is unexported; we test via Upload with different filenames and tar layouts
		It("parses version from VMware-vix-disklib-X.Y.Z-... filename", func() {
			tarGz := test.BuildTarGz(
				test.TarEntry{
					Path:    "lib/x.so",
					Content: "z",
				})
			status, err := srv.Upload(context.Background(),
				"VMware-vix-disklib-12.34.56-12345678.x86_64.tar.gz", bytes.NewReader(tarGz))
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Version).To(Equal("12.34.56"))
		})

		It("extracts version from lib64 libvixDiskLib.so when filename has no version", func() {
			tarGz := test.BuildTarGz(
				test.TarEntry{
					Path:    "vmware-vix-disklib-distrib/lib64/libvixDiskLib.so.8.0.3",
					Content: "library-content",
				})
			status, err := srv.Upload(context.Background(), "vddk.tar.gz", bytes.NewReader(tarGz))
			Expect(err).NotTo(HaveOccurred())
			Expect(status).NotTo(BeNil())
			Expect(status.Version).To(Equal("8.0.3"))
			// Extracted content is under vddk/vmware-vix-disklib-distrib/lib64/
			extracted := filepath.Join(dataDir, "vddk", "vmware-vix-disklib-distrib", "lib64", "libvixDiskLib.so.8.0.3")
			data, err := os.ReadFile(extracted)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(data)).To(Equal("library-content"))
		})

		It("returns error when filename has no version and tar has no lib64 libvixDiskLib.so", func() {
			tarGz := test.BuildTarGz(
				test.TarEntry{
					Path:    "lib/foo.so",
					Content: "x",
				})
			_, err := srv.Upload(context.Background(), "vddk.tar.gz", bytes.NewReader(tarGz))
			Expect(err).To(HaveOccurred())
		})

	})
})
