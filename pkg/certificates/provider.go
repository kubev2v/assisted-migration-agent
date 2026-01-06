package certificates

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"time"
)

func GenerateSelfSignedCertificate(expire time.Time) (*x509.Certificate, *rsa.PrivateKey, error) {
	csr := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().Unix()),
		Issuer: pkix.Name{
			Organization: []string{"Red Hat"},
		},
		Subject: pkix.Name{
			Country:            []string{"US"},
			Organization:       []string{"Red Hat"},
			OrganizationalUnit: []string{"Assisted Migrations"},
		},
		NotBefore:             time.Now(),
		NotAfter:              expire,
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate rsa private key")
	}

	certData, err := x509.CreateCertificate(rand.Reader, csr, csr, privateKey.Public(), privateKey)
	if err != nil {
		return nil, nil, err
	}

	cert, err := x509.ParseCertificate(certData)
	if err != nil {
		return nil, nil, err
	}

	return cert, privateKey, nil
}
