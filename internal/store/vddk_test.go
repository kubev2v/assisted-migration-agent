package store_test

import (
	"context"
	"database/sql"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("VddkStore", func() {
	var (
		ctx context.Context
		s   *store.Store
		db  *sql.DB
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(nil, ":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		s = store.NewStore(db, test.NewMockValidator())
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
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
