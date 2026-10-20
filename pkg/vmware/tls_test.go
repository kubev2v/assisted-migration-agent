package vmware

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/vmware/govmomi/vim25/soap"
)

// generateSelfSignedCert returns a PEM-encoded self-signed certificate for testing.
func generateSelfSignedCert(t *testing.T) ([]byte, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-ca"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse cert: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	return pemBytes, cert
}

func TestNewSoapClient_NoCACert(t *testing.T) {
	u, _ := url.Parse("https://vcenter.example.com/sdk")
	sc, err := NewSoapClient(u, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sc == nil {
		t.Fatal("expected non-nil soap client")
	}
}

func TestNewSoapClient_ValidCACert(t *testing.T) {
	u, _ := url.Parse("https://vcenter.example.com/sdk")
	caPEM, caCert := generateSelfSignedCert(t)

	sc, err := NewSoapClient(u, false, caPEM)
	if err != nil {
		t.Fatalf("unexpected error with valid CA: %v", err)
	}
	if sc == nil {
		t.Fatal("expected non-nil soap client")
	}

	transport := sc.DefaultTransport()
	if transport.TLSClientConfig == nil || transport.TLSClientConfig.RootCAs == nil {
		t.Fatal("expected RootCAs to be set on the TLS config")
	}

	// Verify the specific cert was loaded, not just any non-nil pool.
	expected := x509.NewCertPool()
	expected.AddCert(caCert)
	if !transport.TLSClientConfig.RootCAs.Equal(expected) {
		t.Fatal("RootCAs pool does not contain the expected CA certificate")
	}
}

func TestNewSoapClient_InvalidCACert(t *testing.T) {
	u, _ := url.Parse("https://vcenter.example.com/sdk")
	badPEM := []byte("this is not a valid PEM certificate")

	_, err := NewSoapClient(u, false, badPEM)
	if err == nil {
		t.Fatal("expected error for invalid PEM, got nil")
	}

	// Ensure the bad PEM content is not echoed in the error message.
	if strings.Contains(err.Error(), string(badPEM)) {
		t.Errorf("error message must not contain the raw cert bytes")
	}
}

func TestNewSoapClient_SkipTLS(t *testing.T) {
	u, _ := url.Parse("https://vcenter.example.com/sdk")
	sc, err := NewSoapClient(u, true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	transport := sc.DefaultTransport()
	if transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("expected InsecureSkipVerify=true when skipTLS is true")
	}
}

func TestNewSoapClient_ErrorDoesNotLeakCertContent(t *testing.T) {
	u, _ := url.Parse("https://vcenter.example.com/sdk")
	sentinel := "SENTINELMARKER99"
	badPEM := []byte("-----BEGIN CERTIFICATE-----\n" + sentinel + "\n-----END CERTIFICATE-----\n")

	_, err := NewSoapClient(u, false, badPEM)
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
	if strings.Contains(err.Error(), sentinel) {
		t.Errorf("error must not contain cert content; got: %v", err)
	}
}

func TestGetVCenterThumbprint(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	want := soap.ThumbprintSHA1(server.Certificate())

	for _, tc := range []struct {
		name             string
		skipTLS          bool
		caCert           []byte
		hostnameMismatch bool
		wantUntrusted    bool
	}{
		{name: "skip verification preserves legacy behavior", skipTLS: true},
		{name: "verify with supplied CA", caCert: caPEM},
		{name: "system trust rejects untrusted certificate", wantUntrusted: true},
		{name: "supplied CA does not bypass hostname verification", caCert: caPEM, hostnameMismatch: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.Parse(server.URL + "/sdk")
			if err != nil {
				t.Fatal(err)
			}
			if tc.hostnameMismatch {
				u.Host = "localhost:" + u.Port()
			}
			client, err := NewSoapClient(u, tc.skipTLS, tc.caCert)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(client.CloseIdleConnections)
			got, err := GetVCenterThumbprint(context.Background(), client)
			if tc.wantUntrusted || tc.hostnameMismatch {
				if err == nil || got != "" {
					t.Fatalf("expected certificate rejection and no thumbprint; got %q, %v", got, err)
				}
				if tc.wantUntrusted {
					var unknownCA x509.UnknownAuthorityError
					if !errors.As(err, &unknownCA) {
						t.Fatalf("expected unknown CA error, got %v", err)
					}
				} else {
					var hostnameError x509.HostnameError
					if !errors.As(err, &hostnameError) {
						t.Fatalf("expected hostname verification error, got %v", err)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != want || len(got) != 59 || got != strings.ToUpper(got) {
				t.Fatalf("expected VMware SHA-1 thumbprint %q, got %q", want, got)
			}
		})
	}
}

func TestGetVCenterThumbprint_Canceled(t *testing.T) {
	u, err := url.Parse("https://vcenter.example.com/sdk")
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewSoapClient(u, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.CloseIdleConnections)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := GetVCenterThumbprint(ctx, client)
	if !errors.Is(err, context.Canceled) || got != "" {
		t.Fatalf("expected cancellation and no thumbprint, got %q, %v", got, err)
	}
}
