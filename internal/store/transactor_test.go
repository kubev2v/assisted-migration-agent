package store_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("WithTx", func() {
	var (
		ctx    context.Context
		s      *store.Store
		pool   *store.Pool
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		tmpDir, err = os.MkdirTemp("", "transactor-store-test-*")
		Expect(err).NotTo(HaveOccurred())
		pool = store.NewPool(5 * time.Minute)
		collDB, dbErr := pool.NewDatabase("coll", filepath.Join(tmpDir, "collection.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(dbErr).NotTo(HaveOccurred())
		Expect(collDB.Migrate(ctx, func(mCtx context.Context, sqlDB *sql.DB) error {
			st, stErr := collDB.Store()
			if stErr != nil {
				return stErr
			}
			if pErr := duckdb_parser.New(st.Querier(), test.NewMockValidator()).Init(); pErr != nil {
				return pErr
			}
			return migrations.RunCollection(mCtx, sqlDB, "collection")
		})).To(Succeed())
		pool.Add(collDB)
		s, err = collDB.Store()
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

	It("should commit changes on success", func() {
		err := s.WithTx(ctx, func(txCtx context.Context) error {
			_, err := s.Group().Create(txCtx, models.Group{
				Name:   "tx-group",
				Filter: "memory > 0",
			})
			return err
		})
		Expect(err).NotTo(HaveOccurred())

		groups, err := s.Group().List(ctx, nil, 0, 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(groups).To(HaveLen(1))
		Expect(groups[0].Name).To(Equal("tx-group"))
	})

	It("should rollback changes on error", func() {
		testErr := errors.New("something went wrong")

		err := s.WithTx(ctx, func(txCtx context.Context) error {
			_, err := s.Group().Create(txCtx, models.Group{
				Name:   "rollback-group",
				Filter: "memory > 0",
			})
			Expect(err).NotTo(HaveOccurred())
			return testErr
		})
		Expect(err).To(MatchError(testErr))

		groups, err := s.Group().List(ctx, nil, 0, 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(groups).To(BeEmpty())
	})

	It("should make writes visible within the same transaction", func() {
		err := s.WithTx(ctx, func(txCtx context.Context) error {
			_, err := s.Group().Create(txCtx, models.Group{
				Name:   "visible-in-tx",
				Filter: "memory > 0",
			})
			Expect(err).NotTo(HaveOccurred())

			groups, err := s.Group().List(txCtx, nil, 0, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(groups).To(HaveLen(1))
			Expect(groups[0].Name).To(Equal("visible-in-tx"))

			return nil
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should rollback all writes when a later operation fails", func() {
		err := s.WithTx(ctx, func(txCtx context.Context) error {
			_, err := s.Group().Create(txCtx, models.Group{
				Name:   "first",
				Filter: "memory > 0",
			})
			Expect(err).NotTo(HaveOccurred())

			_, err = s.Group().Create(txCtx, models.Group{
				Name:   "second",
				Filter: "memory > 0",
			})
			Expect(err).NotTo(HaveOccurred())

			return errors.New("abort after two inserts")
		})
		Expect(err).To(HaveOccurred())

		groups, err := s.Group().List(ctx, nil, 0, 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(groups).To(BeEmpty())
	})

	It("should fail fast on nested transactions", func() {
		err := s.WithTx(ctx, func(txCtx context.Context) error {
			_, err := s.Group().Create(txCtx, models.Group{
				Name:   "outer",
				Filter: "memory > 0",
			})
			Expect(err).NotTo(HaveOccurred())

			return s.WithTx(txCtx, func(innerCtx context.Context) error {
				_, err := s.Group().Create(innerCtx, models.Group{
					Name:   "inner",
					Filter: "memory > 0",
				})
				return err
			})
		})
		Expect(err).To(MatchError("nested transactions not supported"))

		groups, err := s.Group().List(ctx, nil, 0, 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(groups).To(BeEmpty())
	})
})
