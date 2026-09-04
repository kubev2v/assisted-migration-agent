package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("OutboxStore", func() {
	var (
		ctx    context.Context
		s      *store.Store
		pool   *store.Pool
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		tmpDir, err = os.MkdirTemp("", "outbox-store-test-*")
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

	Describe("New Event Types", func() {
		Context("GroupInventoryUpsertEvent", func() {
			It("should insert group inventory upsert event", func() {
				groupID := uuid.New().String()
				payload := map[string]interface{}{
					"groupID":   groupID,
					"groupName": "test-group",
					"inventory": map[string]interface{}{
						"vCenterID": "vcenter-01",
						"vms":       []interface{}{map[string]interface{}{"id": "vm1"}},
					},
				}
				payloadBytes, err := json.Marshal(payload)
				Expect(err).NotTo(HaveOccurred())

				event := models.Event{
					Kind: models.GroupInventoryUpsertEvent,
					Data: payloadBytes,
				}

				Expect(s.Outbox().Insert(ctx, event)).To(Succeed())

				events, err := s.Outbox().Get(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(events).To(HaveLen(1))
				Expect(events[0].Kind).To(Equal(models.GroupInventoryUpsertEvent))
				Expect(events[0].Data).To(MatchJSON(payloadBytes))
			})

			It("should verify payload contains groupID, groupName, and inventory", func() {
				groupID := uuid.New().String()
				payload := map[string]interface{}{
					"groupID":   groupID,
					"groupName": "production-vms",
					"inventory": map[string]interface{}{
						"vCenterID": "vcenter-prod",
						"vms":       []interface{}{},
					},
				}
				data, err := json.Marshal(payload)
				Expect(err).NotTo(HaveOccurred())

				event := models.Event{
					Kind: models.GroupInventoryUpsertEvent,
					Data: data,
				}

				Expect(s.Outbox().Insert(ctx, event)).To(Succeed())

				events, err := s.Outbox().Get(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(events).To(HaveLen(1))

				var retrieved map[string]interface{}
				Expect(json.Unmarshal(events[0].Data, &retrieved)).To(Succeed())
				Expect(retrieved).To(HaveKey("groupID"))
				Expect(retrieved).To(HaveKey("groupName"))
				Expect(retrieved).To(HaveKey("inventory"))
				Expect(retrieved).NotTo(HaveKey("vmsCount"), "vmsCount should be extracted from inventory, not stored")
				Expect(retrieved).NotTo(HaveKey("vCenterID"), "vCenterID should be extracted from inventory, not stored")
			})
		})

		Context("GroupInventoryDeleteEvent", func() {
			It("should insert group inventory delete event", func() {
				groupID := uuid.New().String()
				payload := map[string]interface{}{
					"groupID":   groupID,
					"groupName": "test-group",
				}
				payloadBytes, err := json.Marshal(payload)
				Expect(err).NotTo(HaveOccurred())

				event := models.Event{
					Kind: models.GroupInventoryDeleteEvent,
					Data: payloadBytes,
				}

				Expect(s.Outbox().Insert(ctx, event)).To(Succeed())

				events, err := s.Outbox().Get(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(events).To(HaveLen(1))
				Expect(events[0].Kind).To(Equal(models.GroupInventoryDeleteEvent))
				Expect(events[0].Data).To(MatchJSON(payloadBytes))
			})
		})

		Context("Event Ordering", func() {
			It("should process upsert then delete in correct order", func() {
				groupID := uuid.New().String()
				upsertPayload := map[string]interface{}{
					"groupID":   groupID,
					"groupName": "test",
					"inventory": map[string]interface{}{},
				}
				deletePayload := map[string]interface{}{
					"groupID":   groupID,
					"groupName": "test",
				}
				upsertBytes, err := json.Marshal(upsertPayload)
				Expect(err).NotTo(HaveOccurred())
				deleteBytes, err := json.Marshal(deletePayload)
				Expect(err).NotTo(HaveOccurred())

				upsert := models.Event{
					Kind: models.GroupInventoryUpsertEvent,
					Data: upsertBytes,
				}
				delete := models.Event{
					Kind: models.GroupInventoryDeleteEvent,
					Data: deleteBytes,
				}

				Expect(s.Outbox().Insert(ctx, upsert)).To(Succeed())
				Expect(s.Outbox().Insert(ctx, delete)).To(Succeed())

				events, err := s.Outbox().Get(ctx)
				Expect(err).NotTo(HaveOccurred())
				Expect(events).To(HaveLen(2))
				Expect(events[0].Kind).To(Equal(models.GroupInventoryUpsertEvent))
				Expect(events[1].Kind).To(Equal(models.GroupInventoryDeleteEvent))
				Expect(events[0].ID).To(BeNumerically("<", events[1].ID))
			})
		})
	})
})
