package store_test

import (
	"context"
	"database/sql"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("OutboxStore", func() {
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

	Describe("Get", func() {
		It("should return empty slice when no events exist", func() {
			events, err := s.Outbox().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(BeEmpty())
		})

		It("should return events ordered by id", func() {
			e1 := models.Event{Kind: models.InventoryUpdateEvent, Data: []byte(`{"a":1}`)}
			e2 := models.Event{Kind: models.InventoryUpdateEvent, Data: []byte(`{"a":2}`)}
			Expect(s.Outbox().Insert(ctx, e1)).To(Succeed())
			Expect(s.Outbox().Insert(ctx, e2)).To(Succeed())

			events, err := s.Outbox().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(2))
			Expect(events[0].ID).To(BeNumerically("<", events[1].ID))
			Expect(events[0].Data).To(MatchJSON(`{"a":1}`))
			Expect(events[1].Data).To(MatchJSON(`{"a":2}`))
		})
	})

	Describe("Insert", func() {
		It("should insert an event", func() {
			event := models.Event{
				Kind: models.InventoryUpdateEvent,
				Data: []byte(`{"vms":["vm1"]}`),
			}

			Expect(s.Outbox().Insert(ctx, event)).To(Succeed())

			events, err := s.Outbox().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(1))
			Expect(events[0].Kind).To(Equal(models.InventoryUpdateEvent))
			Expect(events[0].Data).To(MatchJSON(`{"vms":["vm1"]}`))
		})

		It("should auto-assign incrementing ids", func() {
			e1 := models.Event{Kind: models.InventoryUpdateEvent, Data: []byte(`{}`)}
			e2 := models.Event{Kind: models.InventoryUpdateEvent, Data: []byte(`{}`)}
			Expect(s.Outbox().Insert(ctx, e1)).To(Succeed())
			Expect(s.Outbox().Insert(ctx, e2)).To(Succeed())

			events, err := s.Outbox().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(2))
			Expect(events[1].ID).To(Equal(events[0].ID + 1))
		})

		It("should participate in transactions", func() {
			err := s.WithTx(ctx, func(txCtx context.Context) error {
				return s.Outbox().Insert(txCtx, models.Event{
					Kind: models.InventoryUpdateEvent,
					Data: []byte(`{"tx":true}`),
				})
			})
			Expect(err).NotTo(HaveOccurred())

			events, err := s.Outbox().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(1))
		})

		It("should rollback insert on transaction failure", func() {
			_ = s.WithTx(ctx, func(txCtx context.Context) error {
				Expect(s.Outbox().Insert(txCtx, models.Event{
					Kind: models.InventoryUpdateEvent,
					Data: []byte(`{"rollback":true}`),
				})).To(Succeed())
				return context.Canceled
			})

			events, err := s.Outbox().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(BeEmpty())
		})
	})

	Describe("Delete", func() {
		It("should delete events up to and including maxID", func() {
			Expect(s.Outbox().Insert(ctx, models.Event{Kind: models.InventoryUpdateEvent, Data: []byte(`{"a":1}`)})).To(Succeed())
			Expect(s.Outbox().Insert(ctx, models.Event{Kind: models.InventoryUpdateEvent, Data: []byte(`{"a":2}`)})).To(Succeed())
			Expect(s.Outbox().Insert(ctx, models.Event{Kind: models.InventoryUpdateEvent, Data: []byte(`{"a":3}`)})).To(Succeed())

			events, err := s.Outbox().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(events).To(HaveLen(3))

			Expect(s.Outbox().Delete(ctx, events[1].ID)).To(Succeed())

			remaining, err := s.Outbox().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(remaining).To(HaveLen(1))
			Expect(remaining[0].Data).To(MatchJSON(`{"a":3}`))
		})

		It("should delete all events when maxID covers all", func() {
			Expect(s.Outbox().Insert(ctx, models.Event{Kind: models.InventoryUpdateEvent, Data: []byte(`{}`)})).To(Succeed())
			Expect(s.Outbox().Insert(ctx, models.Event{Kind: models.InventoryUpdateEvent, Data: []byte(`{}`)})).To(Succeed())

			events, err := s.Outbox().Get(ctx)
			Expect(err).NotTo(HaveOccurred())

			Expect(s.Outbox().Delete(ctx, events[1].ID)).To(Succeed())

			remaining, err := s.Outbox().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(remaining).To(BeEmpty())
		})

		It("should succeed on empty outbox", func() {
			Expect(s.Outbox().Delete(ctx, 0)).To(Succeed())
		})
	})
})
