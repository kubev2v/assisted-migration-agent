package crypto_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
)

var _ = Describe("Crypto", func() {
	var c *crypto.Crypto

	BeforeEach(func() {
		c = crypto.NewCrypto()
	})

	Context("Hash256", func() {
		It("should return a 32-byte hash", func() {
			h := c.Hash256("password")
			Expect(h).To(HaveLen(32))
		})

		It("should return the same hash for the same input", func() {
			Expect(c.Hash256("password")).To(Equal(c.Hash256("password")))
		})

		It("should return different hashes for different inputs", func() {
			Expect(c.Hash256("a")).NotTo(Equal(c.Hash256("b")))
		})
	})

	Context("Encrypt", func() {
		// Given valid credentials and a hash
		// When we encrypt the credentials
		// Then Username and Password should differ from the originals and URL should pass through
		It("should encrypt Username and Password and pass through URL", func() {
			// Arrange
			creds := models.Credentials{
				URL:      "https://vcenter.local/sdk",
				Username: "admin@vsphere.local",
				Password: "s3cret!",
			}

			// Act
			encrypted, err := c.Encrypt(c.Hash256("master-key"), creds)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(encrypted.URL).To(Equal(creds.URL))
			Expect(encrypted.Username).NotTo(Equal(creds.Username))
			Expect(encrypted.Password).NotTo(Equal(creds.Password))
		})

		// Given the same credentials and hash
		// When we encrypt twice
		// Then the ciphertexts should differ due to random nonce and salt
		It("should produce different ciphertexts on each call", func() {
			// Arrange
			creds := models.Credentials{
				URL:      "https://vcenter.local/sdk",
				Username: "admin",
				Password: "pass",
			}
			key := c.Hash256("key")

			// Act
			enc1, err := c.Encrypt(key, creds)
			Expect(err).NotTo(HaveOccurred())

			enc2, err := c.Encrypt(key, creds)
			Expect(err).NotTo(HaveOccurred())

			// Assert
			Expect(enc1.Username).NotTo(Equal(enc2.Username))
			Expect(enc1.Password).NotTo(Equal(enc2.Password))
		})

		// Given credentials with empty fields
		// When we encrypt them
		// Then it should succeed and produce non-empty ciphertext for the encrypted fields
		It("should handle empty username and password", func() {
			// Arrange
			creds := models.Credentials{URL: "https://vc.local", Username: "", Password: ""}

			// Act
			encrypted, err := c.Encrypt(c.Hash256("key"), creds)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(encrypted.URL).To(Equal(creds.URL))
			Expect(encrypted.Username).NotTo(BeEmpty())
			Expect(encrypted.Password).NotTo(BeEmpty())
		})
	})

	Context("Decrypt", func() {
		// Given credentials encrypted with a known hash
		// When we decrypt with the same hash
		// Then we should recover the original credentials
		It("should round-trip encrypt then decrypt", func() {
			// Arrange
			original := models.Credentials{
				URL:      "https://vcenter.local/sdk",
				Username: "admin@vsphere.local",
				Password: "s3cret!P@ssw0rd",
			}
			key := c.Hash256("master-key")

			encrypted, err := c.Encrypt(key, original)
			Expect(err).NotTo(HaveOccurred())

			// Act
			decrypted, err := c.Decrypt(key, encrypted)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(decrypted).To(Equal(original))
		})

		// Given credentials encrypted with one hash
		// When we decrypt with a different hash
		// Then it should return an error
		It("should fail with wrong hash", func() {
			// Arrange
			creds := models.Credentials{
				URL:      "https://vc.local",
				Username: "user",
				Password: "pass",
			}

			encrypted, err := c.Encrypt(c.Hash256("right-key"), creds)
			Expect(err).NotTo(HaveOccurred())

			// Act
			_, err = c.Decrypt(c.Hash256("wrong-key"), encrypted)

			// Assert
			Expect(err).To(HaveOccurred())
		})

		// Given tampered ciphertext
		// When we try to decrypt it
		// Then it should return an error
		It("should fail on tampered ciphertext", func() {
			// Arrange
			creds := models.Credentials{
				URL:      "https://vc.local",
				Username: "user",
				Password: "pass",
			}
			key := c.Hash256("key")

			encrypted, err := c.Encrypt(key, creds)
			Expect(err).NotTo(HaveOccurred())

			encrypted.Username = encrypted.Username[:len(encrypted.Username)-2] + "AA"

			// Act
			_, err = c.Decrypt(key, encrypted)

			// Assert
			Expect(err).To(HaveOccurred())
		})

		// Given invalid base64 in an encrypted field
		// When we try to decrypt
		// Then it should return an error
		It("should fail on invalid base64", func() {
			// Arrange
			encrypted := models.Credentials{
				URL:      "https://vc.local",
				Username: "!!!not-base64!!!",
				Password: "valid-doesnt-matter",
			}

			// Act
			_, err := c.Decrypt(c.Hash256("key"), encrypted)

			// Assert
			Expect(err).To(HaveOccurred())
		})

		// Given a ciphertext that is too short to contain salt+nonce
		// When we try to decrypt
		// Then it should return an error
		It("should fail on truncated ciphertext", func() {
			// Arrange
			encrypted := models.Credentials{
				URL:      "https://vc.local",
				Username: "AAAA",
				Password: "AAAA",
			}

			// Act
			_, err := c.Decrypt(c.Hash256("key"), encrypted)

			// Assert
			Expect(err).To(HaveOccurred())
		})

		// Given credentials with empty username and password
		// When we encrypt and then decrypt
		// Then the round-trip should preserve the empty values
		It("should round-trip empty fields", func() {
			// Arrange
			original := models.Credentials{URL: "https://vc.local", Username: "", Password: ""}
			key := c.Hash256("key")

			encrypted, err := c.Encrypt(key, original)
			Expect(err).NotTo(HaveOccurred())

			// Act
			decrypted, err := c.Decrypt(key, encrypted)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(decrypted).To(Equal(original))
		})

		// Given credentials with unicode characters
		// When we encrypt and then decrypt
		// Then the round-trip should preserve the unicode content
		It("should round-trip unicode content", func() {
			// Arrange
			original := models.Credentials{
				URL:      "https://vc.local",
				Username: "admin@vsphere.local",
				Password: "p@$$wörd-日本語-🔐",
			}
			key := c.Hash256("key")

			encrypted, err := c.Encrypt(key, original)
			Expect(err).NotTo(HaveOccurred())

			// Act
			decrypted, err := c.Decrypt(key, encrypted)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(decrypted).To(Equal(original))
		})
	})

	Context("SkipTLS and CACert pass-through", func() {
		// Given credentials with SkipTLS=true
		// When we encrypt and then decrypt
		// Then SkipTLS must survive the round-trip unchanged
		It("should round-trip SkipTLS=true through Encrypt/Decrypt", func() {
			// Arrange
			original := models.Credentials{
				URL:      "https://vc.local/sdk",
				Username: "admin",
				Password: "pass",
				SkipTLS:  true,
			}
			key := c.Hash256("master-key")

			// Act
			encrypted, err := c.Encrypt(key, original)
			Expect(err).NotTo(HaveOccurred())
			decrypted, err := c.Decrypt(key, encrypted)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(decrypted).To(Equal(original))
		})

		// Given credentials with a non-empty CACert
		// When we encrypt and then decrypt
		// Then CACert must survive the round-trip unchanged
		It("should round-trip CACert through Encrypt/Decrypt", func() {
			// Arrange
			original := models.Credentials{
				URL:      "https://vc.local/sdk",
				Username: "admin",
				Password: "pass",
				CACert:   []byte("-----BEGIN CERTIFICATE-----\nMIIDXTCCAsag...\n-----END CERTIFICATE-----"),
			}
			key := c.Hash256("master-key")

			// Act
			encrypted, err := c.Encrypt(key, original)
			Expect(err).NotTo(HaveOccurred())
			decrypted, err := c.Decrypt(key, encrypted)

			// Assert
			Expect(err).NotTo(HaveOccurred())
			Expect(decrypted).To(Equal(original))
		})
	})
})
