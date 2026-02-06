package certificates_test

import (
	"crypto/x509"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/pkg/certificates"
)

var _ = Describe("Certification Provider", func() {
	Context("self signed certificate", func() {
		It("generates successfully", func() {
			cert, key, err := certificates.GenerateSelfSignedCertificate(time.Now().Add(10 * time.Second))
			Expect(err).To(BeNil())
			Expect(key).ToNot(BeNil())

			data := x509.MarshalPKCS1PrivateKey(key)
			Expect(len(data) > 0).To(BeTrue())

			Expect(cert.Issuer.Organization).Should(ContainElement("Red Hat"))
			Expect(cert.Issuer.OrganizationalUnit).Should(ContainElement("Assisted Migrations"))
		})

		// Given a certificate with a future expiry
		// When we check the certificate validity
		// Then NotBefore should be before NotAfter
		It("has correct validity period", func() {
			expiry := time.Now().Add(24 * time.Hour)
			cert, _, err := certificates.GenerateSelfSignedCertificate(expiry)
			Expect(err).To(BeNil())

			Expect(cert.NotBefore).To(BeTemporally("<", cert.NotAfter))
			Expect(cert.NotAfter).To(BeTemporally("~", expiry, time.Second))
		})

		// Given a generated certificate
		// When we check key usage
		// Then it should support server and client authentication
		It("supports server and client authentication", func() {
			cert, _, err := certificates.GenerateSelfSignedCertificate(time.Now().Add(time.Hour))
			Expect(err).To(BeNil())

			Expect(cert.ExtKeyUsage).To(ContainElement(x509.ExtKeyUsageServerAuth))
			Expect(cert.ExtKeyUsage).To(ContainElement(x509.ExtKeyUsageClientAuth))
			Expect(cert.IsCA).To(BeTrue())
		})

		// Given a generated certificate
		// When we check the key size
		// Then it should be 4096 bits
		It("generates a 4096-bit RSA key", func() {
			_, key, err := certificates.GenerateSelfSignedCertificate(time.Now().Add(time.Hour))
			Expect(err).To(BeNil())

			Expect(key.N.BitLen()).To(Equal(4096))
		})
	})
})
