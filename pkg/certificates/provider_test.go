package certificates_test

import (
	"crypto/x509"
	"net"
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

			Expect(cert.IPAddresses).To(HaveLen(1))
			ip := cert.IPAddresses[0]
			Expect(ip.String()).To(Equal(net.ParseIP("0.0.0.0").String()))
			Expect(cert.Issuer.Organization).Should(ContainElement("Red Hat"))
			Expect(cert.Issuer.OrganizationalUnit).Should(ContainElement("Assisted Migrations"))
		})
	})
})
