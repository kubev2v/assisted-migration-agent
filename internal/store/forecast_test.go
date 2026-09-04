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
)

var _ = Describe("ForecastStore", func() {
	var (
		ctx    context.Context
		s      *store.Store
		pool   *store.Pool
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		tmpDir, err = os.MkdirTemp("", "forecast-store-test-*")
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

	It("should insert and list runs", func() {
		run := models.BenchmarkRun{
			SessionID:      1,
			PairName:       "test-pair",
			SourceDS:       "ds-source",
			TargetDS:       "ds-target",
			Iteration:      1,
			DiskSizeGB:     10,
			DurationSec:    5.5,
			ThroughputMBps: 1861.8,
			Method:         "vm_native",
		}

		Expect(s.Forecast().InsertRun(ctx, run)).To(Succeed())

		runs, err := s.Forecast().ListRuns(ctx, "test-pair")
		Expect(err).NotTo(HaveOccurred())
		Expect(runs).To(HaveLen(1))
		Expect(runs[0].PairName).To(Equal("test-pair"))
		Expect(runs[0].ThroughputMBps).To(Equal(1861.8))
	})

	It("should delete a run", func() {
		run := models.BenchmarkRun{
			SessionID:      1,
			PairName:       "test-pair",
			SourceDS:       "ds-source",
			TargetDS:       "ds-target",
			Iteration:      1,
			DiskSizeGB:     10,
			DurationSec:    5.5,
			ThroughputMBps: 1861.8,
		}

		Expect(s.Forecast().InsertRun(ctx, run)).To(Succeed())

		runs, err := s.Forecast().ListRuns(ctx, "test-pair")
		Expect(err).NotTo(HaveOccurred())
		Expect(runs).NotTo(BeEmpty())

		Expect(s.Forecast().DeleteRun(ctx, runs[0].ID)).To(Succeed())

		runs, err = s.Forecast().ListRuns(ctx, "test-pair")
		Expect(err).NotTo(HaveOccurred())
		Expect(runs).To(BeEmpty())
	})

	It("should return error when deleting non-existent run", func() {
		err := s.Forecast().DeleteRun(ctx, 99999)
		Expect(err).To(HaveOccurred())
	})

	It("should return incrementing session IDs", func() {
		id1, err := s.Forecast().NextSessionID(ctx)
		Expect(err).NotTo(HaveOccurred())

		id2, err := s.Forecast().NextSessionID(ctx)
		Expect(err).NotTo(HaveOccurred())

		Expect(id2).To(BeNumerically(">", id1))
	})
})
