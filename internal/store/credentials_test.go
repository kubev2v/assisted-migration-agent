package store_test

import (
	"context"
	"database/sql"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/test"
)

var _ = Describe("CredentialsStore", func() {
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

	Context("Get", func() {
		// Given no credentials have been stored
		// When we try to retrieve credentials by ID
		// Then it should return a ResourceNotFoundError
		It("should return ResourceNotFoundError when no credentials exist", func() {
			// Arrange — empty database

			// Act
			_, err := s.Credentials().Get(ctx, "vcenter-1")

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		// Given credentials were saved with a known ID
		// When we retrieve them by that ID
		// Then all fields should match what was saved
		It("should return saved credentials", func() {
			// Arrange
			creds := models.Credentials{
				URL:      "https://vcenter.local/sdk",
				Username: "admin@vsphere.local",
				Password: "s3cret",
			}
			err := s.Credentials().Save(ctx, "vcenter-1", creds)
			Expect(err).NotTo(HaveOccurred())

			// Act
			retrieved, err := s.Credentials().Get(ctx, "vcenter-1")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.URL).To(Equal("https://vcenter.local/sdk"))
			Expect(retrieved.Username).To(Equal("admin@vsphere.local"))
			Expect(retrieved.Password).To(Equal("s3cret"))
		})
	})

	Context("Save", func() {
		// Given no prior credentials for an ID
		// When we save new credentials
		// Then they should be retrievable
		It("should save new credentials", func() {
			// Arrange
			creds := models.Credentials{
				URL:      "https://vc.local/sdk",
				Username: "user",
				Password: "pass",
			}

			// Act
			err := s.Credentials().Save(ctx, "vc-new", creds)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			retrieved, err := s.Credentials().Get(ctx, "vc-new")
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.Username).To(Equal("user"))
		})

		// Given credentials already exist for an ID
		// When we save new credentials with the same ID
		// Then the old values should be replaced
		It("should upsert existing credentials", func() {
			// Arrange
			err := s.Credentials().Save(ctx, "vc-1", models.Credentials{
				URL: "https://old.local", Username: "old-user", Password: "old-pass",
			})
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = s.Credentials().Save(ctx, "vc-1", models.Credentials{
				URL: "https://new.local", Username: "new-user", Password: "new-pass",
			})
			Expect(err).NotTo(HaveOccurred())

			// Assert
			retrieved, err := s.Credentials().Get(ctx, "vc-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.URL).To(Equal("https://new.local"))
			Expect(retrieved.Username).To(Equal("new-user"))
			Expect(retrieved.Password).To(Equal("new-pass"))
		})

		// Given credentials saved with a specific ID
		// When we save credentials with a different ID
		// Then both should coexist independently
		It("should store multiple credentials independently", func() {
			// Arrange
			creds1 := models.Credentials{URL: "https://vc1.local", Username: "u1", Password: "p1"}
			creds2 := models.Credentials{URL: "https://vc2.local", Username: "u2", Password: "p2"}

			// Act
			Expect(s.Credentials().Save(ctx, "vc-1", creds1)).To(Succeed())
			Expect(s.Credentials().Save(ctx, "vc-2", creds2)).To(Succeed())

			// Assert
			r1, err := s.Credentials().Get(ctx, "vc-1")
			Expect(err).NotTo(HaveOccurred())
			Expect(r1.Username).To(Equal("u1"))

			r2, err := s.Credentials().Get(ctx, "vc-2")
			Expect(err).NotTo(HaveOccurred())
			Expect(r2.Username).To(Equal("u2"))
		})
	})

	Context("GetPassword", func() {
		// Given no master password has been stored
		// When we try to retrieve it
		// Then it should return a ResourceNotFoundError
		It("should return ResourceNotFoundError when no password exists", func() {
			// Arrange — empty database

			// Act
			_, err := s.Credentials().GetPassword(ctx)

			// Assert
			Expect(err).To(HaveOccurred())
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		// Given a master password was saved
		// When we retrieve it
		// Then it should return the saved value
		It("should return saved password", func() {
			// Arrange
			encoded := "$argon2id$v=19$m=65536,t=1,p=4$c29tZXNhbHQ$somehash"
			Expect(s.Credentials().SavePassword(ctx, encoded)).To(Succeed())

			// Act
			retrieved, err := s.Credentials().GetPassword(ctx)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved).To(Equal(encoded))
		})
	})

	Context("SavePassword", func() {
		// Given a master password already exists
		// When we save a new one
		// Then it should replace the old value
		It("should upsert the master password", func() {
			// Arrange
			Expect(s.Credentials().SavePassword(ctx, "old-password")).To(Succeed())

			// Act
			Expect(s.Credentials().SavePassword(ctx, "new-password")).To(Succeed())

			// Assert
			retrieved, err := s.Credentials().GetPassword(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved).To(Equal("new-password"))
		})
	})

	Context("Delete", func() {
		// Given credentials exist for an ID
		// When we delete them
		// Then they should no longer be retrievable
		It("should delete existing credentials", func() {
			// Arrange
			err := s.Credentials().Save(ctx, "vc-del", models.Credentials{
				URL: "https://vc.local", Username: "u", Password: "p",
			})
			Expect(err).NotTo(HaveOccurred())

			// Act
			err = s.Credentials().Delete(ctx, "vc-del")

			// Assert
			Expect(err).NotTo(HaveOccurred())
			_, err = s.Credentials().Get(ctx, "vc-del")
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		// Given no credentials exist for an ID
		// When we delete that ID
		// Then it should succeed without error
		It("should be idempotent for non-existent ID", func() {
			// Arrange — empty database

			// Act
			err := s.Credentials().Delete(ctx, "does-not-exist")

			// Assert
			Expect(err).NotTo(HaveOccurred())
		})

		// Given two sets of credentials exist
		// When we delete one
		// Then the other should remain intact
		It("should not affect other credentials", func() {
			// Arrange
			Expect(s.Credentials().Save(ctx, "keep", models.Credentials{
				URL: "https://keep.local", Username: "k", Password: "k",
			})).To(Succeed())
			Expect(s.Credentials().Save(ctx, "remove", models.Credentials{
				URL: "https://rm.local", Username: "r", Password: "r",
			})).To(Succeed())

			// Act
			Expect(s.Credentials().Delete(ctx, "remove")).To(Succeed())

			// Assert
			kept, err := s.Credentials().Get(ctx, "keep")
			Expect(err).NotTo(HaveOccurred())
			Expect(kept.Username).To(Equal("k"))
		})
	})
})
