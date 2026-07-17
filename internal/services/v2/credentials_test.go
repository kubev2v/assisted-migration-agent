package v2_test

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

	Context("SetPassword", func() {
		// Given no master password exists
		// When we set an initial password
		// Then it should be verifiable and credentials can be saved
		It("should set initial password", func() {
			// Act
			Expect(srv.SetMasterPassword(ctx, "", "master-key")).To(Succeed())

			// Assert — password is verifiable
			ok, err := srv.VerifyMasterPassword(ctx, "master-key")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())

			// Assert — credentials round-trip
			original := models.Credentials{
				URL:      "https://vcenter.local/sdk",
				Username: "admin@vsphere.local",
				Password: "s3cret",
			}
			Expect(srv.Save(ctx, cr.Hash256("master-key"), "vc-1", original)).To(Succeed())
			retrieved, err := srv.Get(ctx, cr.Hash256("master-key"), "vc-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved).To(Equal(original))
		})

		// Given a master password and multiple credentials exist
		// When we rotate to a new password
		// Then the new password verifies and all credentials decrypt with it
		It("should rotate password and re-encrypt all credentials", func() {
			// Arrange
			Expect(srv.SetMasterPassword(ctx, "", "old-key")).To(Succeed())
			creds := map[string]models.Credentials{
				"vc-1": {URL: "https://vc1.local/sdk", Username: "admin1", Password: "pass1"},
				"vc-2": {URL: "https://vc2.local/sdk", Username: "admin2", Password: "pass2"},
			}
			for id, c := range creds {
				Expect(srv.Save(ctx, cr.Hash256("old-key"), id, c)).To(Succeed())
			}

			// Act
			Expect(srv.SetMasterPassword(ctx, "old-key", "new-key")).To(Succeed())

			// Assert — new password verifies, old does not
			ok, err := srv.VerifyMasterPassword(ctx, "new-key")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeTrue())

			ok, err = srv.VerifyMasterPassword(ctx, "old-key")
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse())

			// Assert — all credentials decrypt with new password
			for id, original := range creds {
				retrieved, err := srv.Get(ctx, cr.Hash256("new-key"), id)
				Expect(err).NotTo(HaveOccurred())
				Expect(retrieved).To(Equal(original))
			}

			// Assert — old password can no longer decrypt
			_, err = srv.Get(ctx, cr.Hash256("old-key"), "vc-1")
			Expect(err).To(HaveOccurred())
		})

		// Given a master password exists
		// When we try to rotate with the wrong old password
		// Then it should fail
		It("should reject wrong old password", func() {
			// Arrange
			Expect(srv.SetMasterPassword(ctx, "", "correct")).To(Succeed())

			// Act
			err := srv.SetMasterPassword(ctx, "wrong", "new-key")

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("old password is incorrect"))
		})
	})

	Context("HasPassword", func() {
		// Given no master password has been set
		// When we check if a password exists
		// Then it should return false
		It("should return false when no password is set", func() {
			// Act
			has, err := srv.HasMasterPassword(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(has).To(BeFalse())
		})

		// Given a master password has been set
		// When we check if a password exists
		// Then it should return true
		It("should return true after setting a password", func() {
			// Arrange
			Expect(srv.SetMasterPassword(ctx, "", "key")).To(Succeed())

			// Act
			has, err := srv.HasMasterPassword(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(has).To(BeTrue())
		})
	})

	Context("VerifyPassword", func() {
		// Given a master password has been set
		// When we verify with the wrong password
		// Then it should return false
		It("should reject wrong password", func() {
			// Arrange
			Expect(srv.SetMasterPassword(ctx, "", "correct")).To(Succeed())

			// Act
			ok, err := srv.VerifyMasterPassword(ctx, "wrong")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(ok).To(BeFalse())
		})

		// Given no master password has been set
		// When we try to verify a password
		// Then it should return an error
		It("should fail when no password is set", func() {
			// Act
			_, err := srv.VerifyMasterPassword(ctx, "anything")

			// Assert
			Expect(err).To(HaveOccurred())
		})
	})

	Context("Save and Get", func() {
		// Given a master password and credentials
		// When we save and then retrieve the credentials
		// Then the decrypted credentials should match the originals
		It("should round-trip credentials through encrypt and decrypt", func() {
			// Arrange
			Expect(srv.SetMasterPassword(ctx, "", "master")).To(Succeed())
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
			Expect(srv.SetMasterPassword(ctx, "", "master")).To(Succeed())
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
			Expect(srv.SetMasterPassword(ctx, "", "key")).To(Succeed())
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
			Expect(srv.SetMasterPassword(ctx, "", "key")).To(Succeed())
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
			Expect(srv.SetMasterPassword(ctx, "", "key")).To(Succeed())
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
