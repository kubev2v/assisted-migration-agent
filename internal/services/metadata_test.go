package services

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vapi/rest"
	_ "github.com/vmware/govmomi/vapi/simulator"
	"github.com/vmware/govmomi/vapi/tags"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
	"github.com/kubev2v/assisted-migration-agent/pkg/work"
)

var _ = Describe("Metadata category names", func() {
	var (
		ctx         context.Context
		pool        *store.Pool
		mainStore   *store.Store
		builder     work.WorkBuilder[models.CollectorStatus, models.CollectorResult]
		categories  work.WorkUnit[models.CollectorStatus, models.CollectorResult]
		result      models.CollectorResult
		manager     *tags.Manager
		credentials models.Credentials
		creds       *CredentialsService
		keyManager  *crypto.KeyManager
		provision   work.WorkUnit[models.CollectorStatus, models.CollectorResult]
	)

	BeforeEach(func() {
		ctx = context.Background()
		model := simulator.VPX()
		Expect(model.Create()).To(Succeed())
		DeferCleanup(model.Remove)
		model.Service.TLS = new(tls.Config)
		model.Service.RegisterEndpoints = true
		model.Service.Listen = &url.URL{User: url.UserPassword("admin", "secret")}
		server := model.Service.NewServer()
		DeferCleanup(server.Close)

		client, err := govmomi.NewClient(ctx, server.URL, true)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(client.Logout, ctx)

		rc := rest.NewClient(client.Client)
		Expect(rc.Login(ctx, server.URL.User)).To(Succeed())
		DeferCleanup(rc.Logout, ctx)
		manager = tags.NewManager(rc)

		pool = store.NewPool(5 * time.Minute)
		DeferCleanup(pool.Close)
		dir := GinkgoT().TempDir()
		mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(dir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, store.ReadWriteDatabase)
		Expect(err).NotTo(HaveOccurred())
		Expect(mainDB.Migrate(ctx, migrations.RunMain)).To(Succeed())
		pool.Add(mainDB)
		mainStore, err = mainDB.Store()
		Expect(err).NotTo(HaveOccurred())

		keyManager, err = crypto.NewKeyManager("")
		Expect(err).NotTo(HaveOccurred())
		creds = NewCredentialsService(mainStore).WithKeyManager(keyManager)
		credentials = models.Credentials{
			URL:      "https://vcenter.example.com",
			Username: "admin",
			Password: "secret",
			SkipTLS:  true,
		}

		factory, err := newVCenterCollectorWorkFactory(creds, pool, dir, nil)
		Expect(err).NotTo(HaveOccurred())
		builder = factory.Build()
		var ok bool
		provision, ok = builder.Next()
		Expect(ok).To(BeTrue())
		DeferCleanup(func() {
			// The simulator client has its own cleanup above.
			result.Client = nil
			Expect(builder.Finalize(ctx, result)).To(Succeed())
		})
		result = models.CollectorResult{
			Client:     client,
			SQLitePath: filepath.Join(dir, "collector's inventory.db"),
		}

		// Prepared inventory replaces credential verification and live collection.
		for range 2 {
			_, ok = builder.Next()
			Expect(ok).To(BeTrue())
		}
		categories, ok = builder.Next()
		Expect(ok).To(BeTrue())
	})

	writeInventory := func(categoryIDs []string) {
		GinkgoHelper()
		escapedPath := strings.ReplaceAll(result.SQLitePath, "'", "''")
		_, err := mainStore.Querier().ExecContext(ctx, fmt.Sprintf("ATTACH '%s' AS inventory_fixture (TYPE sqlite)", escapedPath))
		Expect(err).NotTo(HaveOccurred())
		_, err = mainStore.Querier().ExecContext(ctx, "CREATE TABLE inventory_fixture.VM (IsTemplate INTEGER, Tags VARCHAR)")
		Expect(err).NotTo(HaveOccurred())

		vmTags := make([]map[string]string, 0, len(categoryIDs))
		for _, id := range categoryIDs {
			vmTags = append(vmTags, map[string]string{"categoryID": id, "name": "production"})
		}
		tagsJSON, err := json.Marshal(vmTags)
		Expect(err).NotTo(HaveOccurred())
		_, err = mainStore.Querier().ExecContext(ctx, "INSERT INTO inventory_fixture.VM VALUES (0, ?)", string(tagsJSON))
		Expect(err).NotTo(HaveOccurred())
		_, err = mainStore.Querier().ExecContext(ctx, "DETACH inventory_fixture")
		Expect(err).NotTo(HaveOccurred())
	}

	storedCategoryNames := func() map[string]string {
		GinkgoHelper()
		// Publish the collection without running unrelated ingestion and rightsizing.
		addToPool := categories
		for unit, more := builder.Next(); more; unit, more = builder.Next() {
			if unit.Status().State == models.CollectorStateCollected {
				break
			}
			addToPool = unit
		}
		var err error
		result, err = addToPool.Work(ctx, result)
		Expect(err).NotTo(HaveOccurred())

		db, err := pool.Latest()
		Expect(err).NotTo(HaveOccurred())
		st, err := db.Store()
		Expect(err).NotTo(HaveOccurred())
		rows, err := st.Querier().QueryContext(ctx, "SELECT id, name FROM tag_categories")
		Expect(err).NotTo(HaveOccurred())
		defer func() { Expect(rows.Close()).To(Succeed()) }()

		names := make(map[string]string)
		for rows.Next() {
			var id, name string
			Expect(rows.Scan(&id, &name)).To(Succeed())
			names[id] = name
		}
		Expect(rows.Err()).NotTo(HaveOccurred())
		return names
	}

	It("saves only used category names and skips missing or blank categories", func() {
		err := creds.Save(ctx, keyManager.Key(), credentialsRecordID, credentials)
		Expect(err).NotTo(HaveOccurred())

		result, err = provision.Work(ctx, result)
		Expect(err).NotTo(HaveOccurred())

		var ids []string
		categoryNames := []string{"Owner's; \"name\"", "", "  "}

		for _, name := range categoryNames {
			id, err := manager.CreateCategory(ctx, &tags.Category{Name: name})
			Expect(err).NotTo(HaveOccurred())
			ids = append(ids, id)
		}

		_, err = manager.CreateCategory(ctx, &tags.Category{Name: "unused"})
		Expect(err).NotTo(HaveOccurred())
		ids = append(ids, ids[0], "urn:vmomi:InventoryServiceCategory:missing:GLOBAL")
		writeInventory(ids)

		result, err = categories.Work(ctx, result)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Err).NotTo(HaveOccurred())
		Expect(storedCategoryNames()).To(Equal(map[string]string{ids[0]: "Owner's; \"name\""}))
	})

	It("skips vCenter access when no collected tags use a category", func() {
		err := creds.Save(ctx, keyManager.Key(), credentialsRecordID, credentials)
		Expect(err).NotTo(HaveOccurred())

		result, err = provision.Work(ctx, result)
		Expect(err).NotTo(HaveOccurred())
		writeInventory(nil)

		result.Client = nil // Any attempt to contact vCenter would fail.
		result, err = categories.Work(ctx, result)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Err).NotTo(HaveOccurred())
		Expect(storedCategoryNames()).To(BeEmpty())
	})

	It("reports inventory read failures before contacting vCenter", func() {
		err := creds.Save(ctx, keyManager.Key(), credentialsRecordID, credentials)
		Expect(err).NotTo(HaveOccurred())

		result, err = provision.Work(ctx, result)
		Expect(err).NotTo(HaveOccurred())

		// Leave the inventory file missing and make vCenter access fail.
		result.Client = nil
		result, err = categories.Work(ctx, result)
		Expect(err).To(MatchError(ContainSubstring("reading used tag category IDs")))
		Expect(result.Err).To(MatchError(err))
	})

	Context("when vCenter rejects the credentials", func() {
		It("continues collection without category names", func() {
			credentials.Password = "incorrect"
			err := creds.Save(ctx, keyManager.Key(), credentialsRecordID, credentials)
			Expect(err).NotTo(HaveOccurred())

			result, err = provision.Work(ctx, result)
			Expect(err).NotTo(HaveOccurred())

			id, err := manager.CreateCategory(ctx, &tags.Category{Name: "Environment"})
			Expect(err).NotTo(HaveOccurred())
			writeInventory([]string{id})

			result, err = categories.Work(ctx, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Err).NotTo(HaveOccurred())
			Expect(storedCategoryNames()).To(BeEmpty())
		})
	})
})
