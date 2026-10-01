package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/soap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
)

var _ = Describe("Metadata category names", func() {
	const categoryPath = "/rest/com/vmware/cis/tagging/category"
	for _, tc := range []struct {
		name     string
		failPath string
	}{
		{name: "retains resolved names and skips failed or empty categories"},
		{name: "login failure is nonfatal", failPath: "/rest/com/vmware/cis/session"},
		{name: "listing failure is nonfatal", failPath: categoryPath},
	} {
		It(tc.name, func() {
			ctx := context.Background()
			var loggedOut atomic.Bool
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodDelete {
					loggedOut.Store(true)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if r.URL.Path == tc.failPath || r.URL.Path == categoryPath+"/id:urn:cat2" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				var value any
				switch r.URL.Path {
				case "/rest/com/vmware/cis/session":
					value = "session"
				case categoryPath:
					value = []string{"urn:cat1", "urn:cat2", "urn:cat3", "urn:cat4"}
				case categoryPath + "/id:urn:cat1":
					value = map[string]string{"name": "Owner's; \"name\""}
				case categoryPath + "/id:urn:cat3":
					value = map[string]string{"name": ""}
				case categoryPath + "/id:urn:cat4":
					value = map[string]string{"name": "  "}
				default:
					w.WriteHeader(http.StatusNotFound)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"value": value})
			}))
			defer server.Close()

			pool := store.NewPool(5 * time.Minute)
			defer pool.Close()
			dir := GinkgoT().TempDir()
			mainDB, err := pool.NewDatabase(store.MainDatabaseID, filepath.Join(dir, "agent.duckdb"), time.Now(), store.EagerConnectionInitilization, 0, store.ReadWriteDatabase)
			Expect(err).NotTo(HaveOccurred())
			Expect(mainDB.Migrate(ctx, migrations.RunMain)).NotTo(HaveOccurred())
			pool.Add(mainDB)
			mainStore, err := mainDB.Store()
			Expect(err).NotTo(HaveOccurred())
			keyManager, err := crypto.NewKeyManager("")
			Expect(err).NotTo(HaveOccurred())
			creds := NewCredentialsService(mainStore).WithKeyManager(keyManager)
			Expect(creds.Save(ctx, keyManager.Key(), "credentials", models.Credentials{
				URL: "https://vcenter.example.com", Username: "admin", Password: "secret", SkipTLS: true,
			})).To(Succeed())

			factory, err := newVCenterCollectorWorkFactory(creds, pool, dir, nil)
			Expect(err).NotTo(HaveOccurred())
			builder := factory.Build()
			provision, ok := builder.Next()
			Expect(ok).To(BeTrue())
			result, err := provision.Work(ctx, models.CollectorResult{})
			Expect(err).NotTo(HaveOccurred())
			defer func() {
				result.Client = nil // The test client has no SOAP session to log out.
				Expect(builder.Finalize(ctx, result)).NotTo(HaveOccurred())
			}()
			// Supply the existing collector client without running a full vSphere collection.
			endpoint, err := url.Parse(server.URL)
			Expect(err).NotTo(HaveOccurred())
			result.Client = &govmomi.Client{Client: &vim25.Client{Client: soap.NewClient(endpoint, true)}}
			_, _ = builder.Next() // Verify
			_, _ = builder.Next() // Collect
			categories, ok := builder.Next()
			Expect(ok).To(BeTrue())
			result, err = categories.Work(ctx, result)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Err).NotTo(HaveOccurred())
			Expect(loggedOut.Load()).To(Equal(tc.failPath != "/rest/com/vmware/cis/session"))
			if tc.failPath == "" {
				// Publish the test DB to inspect the saved category names.
				addToPool := categories
				for unit, more := builder.Next(); more; unit, more = builder.Next() {
					if unit.Status().State == models.CollectorStateCollected {
						break
					}
					addToPool = unit
				}
				result, err = addToPool.Work(ctx, result)
				Expect(err).NotTo(HaveOccurred())
				db, err := pool.Latest()
				Expect(err).NotTo(HaveOccurred())
				st, err := db.Store()
				Expect(err).NotTo(HaveOccurred())
				var count int
				Expect(st.Querier().QueryRowContext(ctx, "SELECT count(*) FROM tag_categories").Scan(&count)).NotTo(HaveOccurred())
				Expect(count).To(Equal(1))
				var id, name string
				Expect(st.Querier().QueryRowContext(ctx, "SELECT id, name FROM tag_categories").Scan(&id, &name)).NotTo(HaveOccurred())
				Expect(id).To(Equal("urn:cat1"))
				Expect(name).To(Equal("Owner's; \"name\""))
			}
		})
	}
})
