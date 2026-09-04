package store_test

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

var _ = Describe("VddkStore", func() {
	var (
		ctx    context.Context
		s      *store.Store
		pool   *store.Pool
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		tmpDir, err = os.MkdirTemp("", "vddk-store-test-*")
		Expect(err).NotTo(HaveOccurred())
		pool = store.NewPool(5 * time.Minute)
		mainDB, dbErr := pool.NewDatabase(store.MainDatabaseID, filepath.Join(tmpDir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(dbErr).NotTo(HaveOccurred())
		Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
		pool.Add(mainDB)
		s, err = mainDB.Store()
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		if pool != nil {
			pool.Close()
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	Context("Get", func() {
		It("should return ResourceNotFoundError when no vddk status exists", func() {
			_, err := s.Vddk().Get(ctx)
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("returns saved version and md5", func() {
			status := &models.VddkStatus{
				Version: "8.0.3",
				Md5:     "d41d8cd98f00b204e9800998ecf8427e",
			}
			err := s.Vddk().Save(ctx, status)
			Expect(err).NotTo(HaveOccurred())

			retrieved, err := s.Vddk().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.Version).To(Equal("8.0.3"))
			Expect(retrieved.Md5).To(Equal("d41d8cd98f00b204e9800998ecf8427e"))
		})
	})

	Context("Save", func() {
		It("saves new vddk status", func() {
			status := &models.VddkStatus{
				Version: "9.0.0",
				Md5:     "abc123",
			}
			err := s.Vddk().Save(ctx, status)
			Expect(err).NotTo(HaveOccurred())
		})

		It("upserts existing vddk status", func() {
			err := s.Vddk().Save(ctx, &models.VddkStatus{Version: "8.0.0", Md5: "old"})
			Expect(err).NotTo(HaveOccurred())

			err = s.Vddk().Save(ctx, &models.VddkStatus{Version: "8.0.1", Md5: "new"})
			Expect(err).NotTo(HaveOccurred())

			retrieved, err := s.Vddk().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.Version).To(Equal("8.0.1"))
			Expect(retrieved.Md5).To(Equal("new"))
		})
	})
})
