package v1_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/services"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("CredentialsService", func() {
	var (
		ctx    context.Context
		db     *sql.DB
		srv    *services.CredentialsService
		cr     *crypto.Crypto
		tmpDir string
	)

	BeforeEach(func() {
		ctx = context.Background()
		cr = crypto.NewCrypto()

		var err error
		tmpDir, err = os.MkdirTemp("", "credentials-test-*")
		Expect(err).NotTo(HaveOccurred())

		db, err = store.NewConnection(nil, filepath.Join(tmpDir, "agent.duckdb"))
		Expect(err).NotTo(HaveOccurred())

		st := store.NewStore(db, test.NewMockValidator())
		Expect(st.Migrate(ctx, "")).To(Succeed())
		srv = services.NewCredentialsService(st)
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
		if tmpDir != "" {
			_ = os.RemoveAll(tmpDir)
		}
	})

	Context("Save and Get", func() {
		// Given a credential encryption key and credentials
		// When we save and then retrieve the credentials
		// Then the decrypted credentials should match the originals
		It("should round-trip credentials through encrypt and decrypt", func() {
			// Arrange
			original := models.Credentials{
				URL:      "https://vcenter.local/sdk",
				Username: "admin@vsphere.local",
				Password: "s3cret",
			}

			// Act
			Expect(srv.Save(ctx, cr.Hash256("master"), "vc-1", original)).To(Succeed())
			retrieved, err := srv.Get(ctx, cr.Hash256("master"), "vc-1")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved).To(Equal(original))
		})

		// Given credentials saved with one password
		// When we try to get them with a different password
		// Then it should return an error
		It("should fail to decrypt with wrong password", func() {
			// Arrange
			Expect(srv.Save(ctx, cr.Hash256("master"), "vc-1", models.Credentials{
				URL: "https://vc.local", Username: "u", Password: "p",
			})).To(Succeed())

			// Act
			_, err := srv.Get(ctx, cr.Hash256("wrong"), "vc-1")

			// Assert
			Expect(err).To(HaveOccurred())
		})

		// Given no credentials exist for an ID
		// When we try to get them
		// Then it should return a ResourceNotFoundError
		It("should return not found for missing credentials", func() {
			// Act
			_, err := srv.Get(ctx, cr.Hash256("master"), "nonexistent")

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		// Given credentials saved with the URL
		// When we retrieve them
		// Then the URL should be unchanged (not encrypted)
		It("should preserve URL without encryption", func() {
			// Arrange
			original := models.Credentials{
				URL:      "https://vcenter.local/sdk",
				Username: "admin",
				Password: "pass",
			}

			// Act
			Expect(srv.Save(ctx, cr.Hash256("key"), "vc-1", original)).To(Succeed())
			retrieved, err := srv.Get(ctx, cr.Hash256("key"), "vc-1")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.URL).To(Equal(original.URL))
		})
	})

	Context("List", func() {
		// Given no credentials exist
		// When we list credentials
		// Then it should return an empty list
		It("should return empty list when no credentials exist", func() {
			// Act
			ids, err := srv.List(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(ids).To(BeEmpty())
		})

		// Given multiple credentials have been saved
		// When we list credentials
		// Then it should return all IDs in order
		It("should return all credential IDs", func() {
			// Arrange
			Expect(srv.Save(ctx, cr.Hash256("key"), "vc-b", models.Credentials{URL: "b", Username: "u", Password: "p"})).To(Succeed())
			Expect(srv.Save(ctx, cr.Hash256("key"), "vc-a", models.Credentials{URL: "a", Username: "u", Password: "p"})).To(Succeed())

			// Act
			ids, err := srv.List(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(ids).To(Equal([]string{"vc-a", "vc-b"}))
		})
	})

	Context("Status", func() {
		It("should return ResourceNotFoundError when no credentials stored", func() {
			keyMgr, err := crypto.NewKeyManager("")
			Expect(err).NotTo(HaveOccurred())
			srv = srv.WithKeyManager(keyMgr)

			_, _, err = srv.Status(ctx)
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("should return ResourceNotFoundError when key manager not set", func() {
			_, _, err := srv.Status(ctx)
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})
	})

	Context("Delete", func() {
		// Given saved credentials
		// When we delete them
		// Then they should no longer be retrievable
		It("should delete credentials", func() {
			// Arrange
			Expect(srv.Save(ctx, cr.Hash256("key"), "vc-1", models.Credentials{URL: "u", Username: "u", Password: "p"})).To(Succeed())

			// Act
			Expect(srv.Delete(ctx, "vc-1")).To(Succeed())

			// Assert
			_, err := srv.Get(ctx, cr.Hash256("key"), "vc-1")
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		// Given no credentials for an ID
		// When we delete it
		// Then it should succeed (idempotent)
		It("should be idempotent", func() {
			// Act
			err := srv.Delete(ctx, "nonexistent")

			// Assert
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
