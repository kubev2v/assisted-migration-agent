package store_test

import (
	"context"
	"database/sql"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("WithTx", func() {
	var (
		ctx context.Context
		s   *store.Store
		db  *sql.DB
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(":memory:")
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
})
