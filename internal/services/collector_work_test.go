package services

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/kubev2v/migration-planner/pkg/opa"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
)

var _ = Describe("Collector work collection time", func() {
	var (
		ctx         context.Context
		dataDir     string
		sqlitePath  string
		pool        *store.Pool
		credentials *CredentialsService
	)

	BeforeEach(func() {
		By("preparing the main database and vCenter credentials")
		ctx = context.Background()
		dataDir = GinkgoT().TempDir()
		pool = store.NewPool(time.Minute)
		DeferCleanup(pool.Close)
		mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(dataDir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(mainDB.Migrate(ctx, migrations.RunMain)).NotTo(HaveOccurred())
		pool.Add(mainDB)
		mainStore, err := mainDB.Store()
		Expect(err).NotTo(HaveOccurred())
		key, err := crypto.NewKeyManager("")
		Expect(err).NotTo(HaveOccurred())
		credentials = NewCredentialsService(mainStore).WithKeyManager(key)
		Expect(credentials.Save(ctx, key.Key(), credentialsRecordID, models.Credentials{
			URL:      "https://vcenter.local/sdk",
			Username: "user",
			Password: "password",
		})).To(Succeed())

		By("preparing a SQLite export with one virtual machine")
		sqlitePath = filepath.Join(dataDir, "source.db")
		_, err = mainStore.Querier().ExecContext(ctx, "ATTACH '"+sqlitePath+"' AS fixture (TYPE sqlite)")
		Expect(err).NotTo(HaveOccurred())
		for _, statement := range []string{
			`CREATE TABLE fixture.About (APIVersion VARCHAR, Product VARCHAR, InstanceUuid VARCHAR)`,
			`INSERT INTO fixture.About (APIVersion, Product, InstanceUuid)
			VALUES ('8.0', 'vCenter', 'vc-1')`,
			`CREATE TABLE fixture.Datacenter (ID VARCHAR, Name VARCHAR)`,
			`CREATE TABLE fixture.Folder (ID VARCHAR, Datacenter VARCHAR)`,
			`CREATE TABLE fixture.Cluster (
			ID VARCHAR, Name VARCHAR, Parent VARCHAR, Variant VARCHAR,
			DasEnabled BOOLEAN, DrsEnabled BOOLEAN, DrsBehavior VARCHAR
		)`,
			`INSERT INTO fixture.Cluster (ID, Name, Parent, Variant, DasEnabled, DrsEnabled, DrsBehavior)
			VALUES ('cluster-1', 'cluster1', '{}', 'ClusterComputeResource', false, false, '')`,
			`CREATE TABLE fixture.Host (
			ID VARCHAR, Cluster VARCHAR, CpuCores INTEGER, CpuSockets INTEGER,
			MemoryBytes BIGINT, Model VARCHAR, Vendor VARCHAR, Datastores VARCHAR,
			VMotionSupported BOOLEAN, StorageVMotionSupported BOOLEAN
		)`,
			`INSERT INTO fixture.Host (
			ID, Cluster, CpuCores, CpuSockets, MemoryBytes, Model, Vendor,
			Datastores, VMotionSupported, StorageVMotionSupported
		) VALUES ('host-1', 'cluster-1', 8, 2, 34359738368, 'ESXi', 'VMware', '[]', true, true)`,
			`CREATE TABLE fixture.VM (
			ID VARCHAR, Name VARCHAR, Folder VARCHAR, Host VARCHAR, UUID VARCHAR, Firmware VARCHAR,
			PowerState VARCHAR, ConnectionState VARCHAR, FaultToleranceEnabled INTEGER,
			CpuCount INTEGER, MemoryMB INTEGER, GuestName VARCHAR, GuestNameFromVmwareTools VARCHAR,
			HostName VARCHAR, IpAddress VARCHAR, StorageUsed BIGINT, IsTemplate INTEGER,
			ChangeTrackingEnabled INTEGER, DiskEnableUuid INTEGER, Disks VARCHAR, NICs VARCHAR,
			CpuHotAddEnabled INTEGER, CpuHotRemoveEnabled INTEGER, CoresPerSocket INTEGER,
			MemoryHotAddEnabled INTEGER, BalloonedMemory INTEGER, GuestApps VARCHAR, GuestNetworks VARCHAR
		)`,
			`INSERT INTO fixture.VM (
			ID, Name, Folder, Host, UUID, Firmware,
			PowerState, ConnectionState, FaultToleranceEnabled,
			CpuCount, MemoryMB, GuestName, GuestNameFromVmwareTools,
			HostName, IpAddress, StorageUsed, IsTemplate,
			ChangeTrackingEnabled, DiskEnableUuid, Disks, NICs,
			CpuHotAddEnabled, CpuHotRemoveEnabled, CoresPerSocket,
			MemoryHotAddEnabled, BalloonedMemory, GuestApps, GuestNetworks
		) VALUES (
			'vm-1', 'vm1', '/', 'host-1', 'uuid-1', 'efi',
			'poweredOn', 'connected', 0,
			2, 4096, 'Linux', 'Linux',
			'', '', 0, 0,
			0, 0, '[]', '[]',
			0, 0, 2,
			0, 0, '[]', '[]'
		)`,
			`DETACH fixture`,
		} {
			_, err := mainStore.Querier().ExecContext(ctx, statement)
			Expect(err).NotTo(HaveOccurred())
		}

	})

	It("records the collection start time in the returned and saved inventory", func() {
		By("provisioning the collection database and capturing its start time")
		validator, err := opa.NewValidator(map[string]string{"test.rego": "package io.konveyor.forklift.vmware\nconcerns := []"})
		Expect(err).NotTo(HaveOccurred())
		factory, err := newVCenterCollectorWorkFactory(credentials, pool, dataDir, validator)
		Expect(err).NotTo(HaveOccurred())
		builder := factory.Build()
		DeferCleanup(builder.Finalize, ctx, models.CollectorResult{})
		provision, ok := builder.Next()
		Expect(ok).To(BeTrue())
		beforeProvision := time.Now().UTC().Truncate(time.Microsecond)
		result, err := provision.Work(ctx, models.CollectorResult{})
		Expect(err).NotTo(HaveOccurred())
		afterProvision := time.Now().UTC()

		By("ingesting the prepared export and removing its SQLite file")
		// The prepared export replaces the live vCenter stages.
		for _, stage := range []string{"verify credentials", "collect inventory", "look up tag categories"} {
			_, ok = builder.Next()
			Expect(ok).To(BeTrue(), "expected pipeline stage: %s", stage)
		}
		ingest, ok := builder.Next()
		Expect(ok).To(BeTrue())
		result.SQLitePath = sqlitePath
		_, err = ingest.Work(ctx, result)
		Expect(err).NotTo(HaveOccurred())
		_, err = os.Stat(sqlitePath)
		Expect(os.IsNotExist(err)).To(BeTrue())

		files, err := filepath.Glob(filepath.Join(dataDir, "collection_*.duckdb"))
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(HaveLen(1))

		By("building inventory without running unrelated collection stages")
		for _, stage := range []string{
			"detect applications",
			"create rightsizing report",
			"query and persist metrics",
			"save metrics warnings",
			"compute utilization",
			"sync previous collection",
		} {
			_, ok = builder.Next()
			Expect(ok).To(BeTrue(), "expected pipeline stage: %s", stage)
		}
		inventoryStage, ok := builder.Next()
		Expect(ok).To(BeTrue())
		result, err = inventoryStage.Work(ctx, result)
		Expect(err).NotTo(HaveOccurred())

		By("checking that inventory uses the collection start time")
		var inv struct {
			CollectionTime *time.Time `json:"collection_time"`
		}
		Expect(json.Unmarshal(result.Inventory, &inv)).To(Succeed())
		Expect(inv.CollectionTime).NotTo(BeNil())
		collectedAt := *inv.CollectionTime
		Expect(collectedAt).To(BeTemporally(">=", beforeProvision))
		Expect(collectedAt).To(BeTemporally("<=", afterProvision))

		By("checking that the same inventory was saved to the collection database")
		db, err := store.NewConnection(nil, files[0])
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(db.Close)
		var stored []byte
		Expect(db.QueryRowContext(ctx, "SELECT data FROM inventory LIMIT 1").Scan(&stored)).To(Succeed())
		Expect(stored).To(Equal(result.Inventory))
	})
})
