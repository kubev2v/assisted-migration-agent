package v2_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/migration-planner/pkg/duckdb_parser"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	v2 "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
)

var _ = Describe("EventService", func() {
	var (
		ctx    context.Context
		pool   *store.Pool
		tmpDir string
		st     *store.Store2
		srv    *v2.EventService
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		tmpDir, err = os.MkdirTemp("", "event-test-*")
		Expect(err).NotTo(HaveOccurred())

		pool = store.NewPool(5 * time.Minute)
		database, err := pool.NewDatabase("test", filepath.Join(tmpDir, "test.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())

		st, err = database.Store()
		Expect(err).NotTo(HaveOccurred())
		Expect(duckdb_parser.New(st.Querier(), nil).Init()).To(Succeed())
		Expect(database.Migrate(ctx, func(ctx context.Context, db *sql.DB) error {
			return migrations.RunCollection(ctx, db, "test")
		})).To(Succeed())

		srv = v2.NewEventService(st)
	})

	AfterEach(func() {
		if pool != nil {
			pool.Close()
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	It("inserts and lists an inventory update event", func() {
		Expect(srv.AddInventoryUpdateEvent(ctx, []byte(`{"foo":"bar"}`))).To(Succeed())

		events, err := srv.Events(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(events).To(HaveLen(1))
		Expect(events[0].Kind).To(Equal(models.InventoryUpdateEvent))
		Expect(events[0].Data).To(MatchJSON(`{"foo":"bar"}`))
	})

	It("inserts a group inventory upsert event", func() {
		Expect(srv.AddGroupInventoryEvent(ctx, []byte(`{"groupID":"g1"}`))).To(Succeed())

		events, err := srv.Events(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(events).To(HaveLen(1))
		Expect(events[0].Kind).To(Equal(models.GroupInventoryUpsertEvent))
	})

	It("inserts a group inventory delete event", func() {
		Expect(srv.AddGroupInventoryDeleteEvent(ctx, []byte(`{"groupID":"g1"}`))).To(Succeed())

		events, err := srv.Events(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(events).To(HaveLen(1))
		Expect(events[0].Kind).To(Equal(models.GroupInventoryDeleteEvent))
	})

	It("deletes events up to and including maxID", func() {
		Expect(srv.AddInventoryUpdateEvent(ctx, []byte(`{}`))).To(Succeed())
		Expect(srv.AddInventoryUpdateEvent(ctx, []byte(`{}`))).To(Succeed())

		events, err := srv.Events(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(events).To(HaveLen(2))

		Expect(srv.Delete(ctx, events[0].ID)).To(Succeed())

		remaining, err := srv.Events(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(remaining).To(HaveLen(1))
		Expect(remaining[0].ID).To(Equal(events[1].ID))
	})
})
