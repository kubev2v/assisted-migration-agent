package store_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCredentialsStore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Credentials Store Suite")
}

var _ = Describe("CredentialsStore", func() {
	var (
		ctx   context.Context
		s     *store.Store
		db    *sql.DB
		creds *models.Credentials
	)

	BeforeEach(func() {
		ctx = context.Background()

		var err error
		db, err = store.NewDB(":memory:")
		Expect(err).NotTo(HaveOccurred())

		err = migrations.Run(ctx, db)
		Expect(err).NotTo(HaveOccurred())

		s = store.NewStore(db)

		creds = &models.Credentials{
			URL:                  "https://vcenter.example.com",
			Username:             "admin",
			Password:             "secret123",
			IsDataSharingAllowed: true,
		}
	})

	AfterEach(func() {
		if db != nil {
			_ = db.Close()
		}
	})

	Describe("Save", func() {
		It("should save credentials successfully", func() {
			err := s.Credentials().Save(ctx, creds)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should update credentials on second save (upsert)", func() {
			// First save
			err := s.Credentials().Save(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			// Update credentials
			updatedCreds := &models.Credentials{
				URL:                  "https://new-vcenter.example.com",
				Username:             "newadmin",
				Password:             "newsecret",
				IsDataSharingAllowed: false,
			}
			err = s.Credentials().Save(ctx, updatedCreds)
			Expect(err).NotTo(HaveOccurred())

			// Verify updated values
			retrieved, err := s.Credentials().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.URL).To(Equal("https://new-vcenter.example.com"))
			Expect(retrieved.Username).To(Equal("newadmin"))
			Expect(retrieved.Password).To(Equal("newsecret"))
			Expect(retrieved.IsDataSharingAllowed).To(BeFalse())
		})
	})

	Describe("Get", func() {
		It("should return ResourceNotFoundError when no credentials exist", func() {
			_, err := s.Credentials().Get(ctx)
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("should retrieve saved credentials", func() {
			err := s.Credentials().Save(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			retrieved, err := s.Credentials().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.URL).To(Equal(creds.URL))
			Expect(retrieved.Username).To(Equal(creds.Username))
			Expect(retrieved.Password).To(Equal(creds.Password))
			Expect(retrieved.IsDataSharingAllowed).To(Equal(creds.IsDataSharingAllowed))
		})

		It("should have timestamps set by database", func() {
			err := s.Credentials().Save(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			retrieved, err := s.Credentials().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.CreatedAt).NotTo(BeZero())
			Expect(retrieved.UpdatedAt).NotTo(BeZero())
		})
	})

	Describe("Delete", func() {
		It("should delete existing credentials", func() {
			// Save first
			err := s.Credentials().Save(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			// Delete
			err = s.Credentials().Delete(ctx)
			Expect(err).NotTo(HaveOccurred())

			// Verify deleted - Get should return ResourceNotFoundError
			_, err = s.Credentials().Get(ctx)
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("should return ResourceNotFoundError after delete", func() {
			// Save first
			err := s.Credentials().Save(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			// Delete
			err = s.Credentials().Delete(ctx)
			Expect(err).NotTo(HaveOccurred())

			// Get should return ResourceNotFoundError
			_, err = s.Credentials().Get(ctx)
			Expect(srvErrors.IsResourceNotFoundError(err)).To(BeTrue())
		})

		It("should not error when deleting non-existent credentials", func() {
			err := s.Credentials().Delete(ctx)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("Save after Delete", func() {
		It("should allow saving new credentials after delete", func() {
			// Save
			err := s.Credentials().Save(ctx, creds)
			Expect(err).NotTo(HaveOccurred())

			// Delete
			err = s.Credentials().Delete(ctx)
			Expect(err).NotTo(HaveOccurred())

			// Save new credentials
			newCreds := &models.Credentials{
				URL:                  "https://another-vcenter.example.com",
				Username:             "anotheruser",
				Password:             "anotherpass",
				IsDataSharingAllowed: false,
			}
			err = s.Credentials().Save(ctx, newCreds)
			Expect(err).NotTo(HaveOccurred())

			// Verify new credentials
			retrieved, err := s.Credentials().Get(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(retrieved.URL).To(Equal(newCreds.URL))
			Expect(retrieved.Username).To(Equal(newCreds.Username))
		})
	})
})
