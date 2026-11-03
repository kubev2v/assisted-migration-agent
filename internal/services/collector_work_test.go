package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kubev2v/migration-planner/pkg/opa"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
)

func TestCollectorWork_CollectionTime(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()
	dataDir := t.TempDir()
	pool := store.NewPool(time.Minute)
	defer pool.Close()
	mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(dataDir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(mainDB.Migrate(ctx, func(ctx context.Context, db *sql.DB) error { return migrations.RunMain(ctx, db) })).NotTo(HaveOccurred())
	pool.Add(mainDB)
	mainStore, err := mainDB.Store()
	g.Expect(err).NotTo(HaveOccurred())
	key, err := crypto.NewKeyManager("")
	g.Expect(err).NotTo(HaveOccurred())
	credentials := NewCredentialsService(mainStore).WithKeyManager(key)
	g.Expect(credentials.Save(ctx, key.Key(), credentialsRecordID, models.Credentials{URL: "https://vcenter.local/sdk", Username: "user", Password: "password"})).NotTo(HaveOccurred())

	sqlitePath := filepath.Join(dataDir, "source.db")
	_, err = mainStore.Querier().ExecContext(ctx, "ATTACH '"+sqlitePath+"' AS fixture (TYPE sqlite)")
	g.Expect(err).NotTo(HaveOccurred())
	for _, statement := range []string{
		`CREATE TABLE fixture.About (APIVersion VARCHAR, Product VARCHAR, InstanceUuid VARCHAR)`,
		`INSERT INTO fixture.About VALUES ('8.0', 'vCenter', 'vc-1')`,
		`CREATE TABLE fixture.Datacenter (ID VARCHAR, Name VARCHAR)`,
		`CREATE TABLE fixture.Folder (ID VARCHAR, Datacenter VARCHAR)`,
		`CREATE TABLE fixture.Cluster (ID VARCHAR, Name VARCHAR, Parent VARCHAR, Variant VARCHAR, DasEnabled BOOLEAN, DrsEnabled BOOLEAN, DrsBehavior VARCHAR)`,
		`INSERT INTO fixture.Cluster VALUES ('cluster-1', 'cluster1', '{}', 'ClusterComputeResource', false, false, '')`,
		`CREATE TABLE fixture.Host (ID VARCHAR, Cluster VARCHAR, CpuCores INTEGER, CpuSockets INTEGER, MemoryBytes BIGINT, Model VARCHAR, Vendor VARCHAR, Datastores VARCHAR, VMotionSupported BOOLEAN, StorageVMotionSupported BOOLEAN)`,
		`INSERT INTO fixture.Host VALUES ('host-1', 'cluster-1', 8, 2, 34359738368, 'ESXi', 'VMware', '[]', true, true)`,
		`CREATE TABLE fixture.VM (
                    ID VARCHAR, Name VARCHAR, Folder VARCHAR, Host VARCHAR, UUID VARCHAR, Firmware VARCHAR,
                    PowerState VARCHAR, ConnectionState VARCHAR, FaultToleranceEnabled INTEGER,
                    CpuCount INTEGER, MemoryMB INTEGER, GuestName VARCHAR, GuestNameFromVmwareTools VARCHAR,
                    HostName VARCHAR, IpAddress VARCHAR, StorageUsed BIGINT, IsTemplate INTEGER,
                    ChangeTrackingEnabled INTEGER, DiskEnableUuid INTEGER, Disks VARCHAR, NICs VARCHAR,
                    CpuHotAddEnabled INTEGER, CpuHotRemoveEnabled INTEGER, CoresPerSocket INTEGER,
                    MemoryHotAddEnabled INTEGER, BalloonedMemory INTEGER, GuestApps VARCHAR, GuestNetworks VARCHAR
                )`,
		`INSERT INTO fixture.VM VALUES ('vm-1', 'vm1', '/', 'host-1', 'uuid-1', 'efi', 'poweredOn', 'connected', 0, 2, 4096, 'Linux', 'Linux', '', '', 0, 0, 0, 0, '[]', '[]', 0, 0, 2, 0, 0, '[]', '[]')`,
		`DETACH fixture`,
	} {
		_, err := mainStore.Querier().ExecContext(ctx, statement)
		g.Expect(err).NotTo(HaveOccurred())
	}

	validator, err := opa.NewValidator(map[string]string{"test.rego": "package io.konveyor.forklift.vmware\nconcerns := []"})
	g.Expect(err).NotTo(HaveOccurred())
	factory, err := newVCenterCollectorWorkFactory(credentials, pool, dataDir, validator)
	g.Expect(err).NotTo(HaveOccurred())
	builder := factory.Build()
	defer func() { _ = builder.Finalize(ctx, models.CollectorResult{}) }()
	provision, ok := builder.Next()
	g.Expect(ok).To(BeTrue())
	before := time.Now().UTC().Truncate(time.Microsecond)
	result, err := provision.Work(ctx, models.CollectorResult{})
	g.Expect(err).NotTo(HaveOccurred())
	after := time.Now().UTC()
	// Use a prepared SQLite export; skip verification, collection and category lookup.
	for range 3 {
		_, ok = builder.Next()
		g.Expect(ok).To(BeTrue())
	}
	ingest, ok := builder.Next()
	g.Expect(ok).To(BeTrue())
	result.SQLitePath = sqlitePath
	_, err = ingest.Work(ctx, result)
	g.Expect(err).NotTo(HaveOccurred())
	_, err = os.Stat(sqlitePath)
	g.Expect(os.IsNotExist(err)).To(BeTrue())

	files, err := filepath.Glob(filepath.Join(dataDir, "collection_*.duckdb"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(files).To(HaveLen(1))
	// Skip unrelated application, metrics, and sync stages to exercise the inventory build.
	for range 6 {
		_, ok = builder.Next()
		g.Expect(ok).To(BeTrue())
	}
	inventoryStage, ok := builder.Next()
	g.Expect(ok).To(BeTrue())
	result, err = inventoryStage.Work(ctx, result)
	g.Expect(err).NotTo(HaveOccurred())
	var inv struct {
		CollectionTime *time.Time `json:"collection_time"`
	}
	g.Expect(json.Unmarshal(result.Inventory, &inv)).To(Succeed())
	g.Expect(inv.CollectionTime).NotTo(BeNil())
	collectedAt := *inv.CollectionTime
	g.Expect(collectedAt.Before(before)).To(BeFalse())
	g.Expect(collectedAt.After(after)).To(BeFalse())
	db, err := store.NewConnection(nil, files[0])
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = db.Close() }()
	var stored []byte
	g.Expect(db.QueryRowContext(ctx, "SELECT data FROM inventory LIMIT 1").Scan(&stored)).To(Succeed())
	g.Expect(stored).To(Equal(result.Inventory))
}
