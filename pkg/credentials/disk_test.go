package credentials_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/pkg/credentials"
)

var _ = Describe("DiskStore", func() {
	var (
		tmpDir string
		store  *credentials.DiskStore
	)

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "credentials-test-*")
		Expect(err).NotTo(HaveOccurred())
		store = credentials.NewDiskStore(tmpDir)
	})

	AfterEach(func() {
		if tmpDir != "" {
			os.RemoveAll(tmpDir)
		}
	})

	Describe("Save and Load", func() {
		It("should save and load credentials", func() {
			creds := models.VCenterCredentials{
				URL:      "https://vcenter.example.com",
				Username: "admin@vsphere.local",
				Password: "secret123",
			}

			err := store.Save(creds)
			Expect(err).NotTo(HaveOccurred())
			Expect(store.Exists()).To(BeTrue())

			loaded, err := store.Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded.URL).To(Equal(creds.URL))
			Expect(loaded.Username).To(Equal(creds.Username))
			Expect(loaded.Password).To(Equal(creds.Password))
		})

		It("should overwrite existing credentials", func() {
			creds1 := models.VCenterCredentials{
				URL:      "https://vcenter1.example.com",
				Username: "admin1",
				Password: "pass1",
			}
			err := store.Save(creds1)
			Expect(err).NotTo(HaveOccurred())

			creds2 := models.VCenterCredentials{
				URL:      "https://vcenter2.example.com",
				Username: "admin2",
				Password: "pass2",
			}
			err = store.Save(creds2)
			Expect(err).NotTo(HaveOccurred())

			loaded, err := store.Load()
			Expect(err).NotTo(HaveOccurred())
			Expect(loaded.URL).To(Equal(creds2.URL))
			Expect(loaded.Username).To(Equal(creds2.Username))
			Expect(loaded.Password).To(Equal(creds2.Password))
		})
	})

	Describe("Load", func() {
		It("should return ErrNotFound when no credentials exist", func() {
			_, err := store.Load()
			Expect(err).To(MatchError(credentials.ErrNotFound))
		})
	})

	Describe("Exists", func() {
		It("should return false when no credentials exist", func() {
			Expect(store.Exists()).To(BeFalse())
		})

		It("should return true after saving credentials", func() {
			creds := models.VCenterCredentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "pass",
			}
			err := store.Save(creds)
			Expect(err).NotTo(HaveOccurred())
			Expect(store.Exists()).To(BeTrue())
		})
	})

	Describe("Delete", func() {
		It("should delete existing credentials", func() {
			creds := models.VCenterCredentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "pass",
			}
			err := store.Save(creds)
			Expect(err).NotTo(HaveOccurred())
			Expect(store.Exists()).To(BeTrue())

			err = store.Delete()
			Expect(err).NotTo(HaveOccurred())
			Expect(store.Exists()).To(BeFalse())

			_, err = store.Load()
			Expect(err).To(MatchError(credentials.ErrNotFound))
		})

		It("should not error when deleting non-existent credentials", func() {
			err := store.Delete()
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("File permissions", func() {
		It("should create file with restrictive permissions", func() {
			creds := models.VCenterCredentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "secret",
			}
			err := store.Save(creds)
			Expect(err).NotTo(HaveOccurred())

			filePath := filepath.Join(tmpDir, "credentials.json")
			info, err := os.Stat(filePath)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))
		})
	})

	Describe("Data folder creation", func() {
		It("should create nested directories if they don't exist", func() {
			nestedDir := filepath.Join(tmpDir, "nested", "data", "folder")
			nestedStore := credentials.NewDiskStore(nestedDir)

			creds := models.VCenterCredentials{
				URL:      "https://vcenter.example.com",
				Username: "admin",
				Password: "pass",
			}
			err := nestedStore.Save(creds)
			Expect(err).NotTo(HaveOccurred())

			_, err = os.Stat(nestedDir)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
