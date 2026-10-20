package vmware

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/vmware/govmomi/vim25/soap"
)

// NewSoapClient creates a govmomi SOAP client with TLS configured from the
// provided parameters. Pass insecure=true only when the caller has explicitly
// opted out of certificate verification (SkipTLS=true on Credentials).
// If caCert is non-empty it must be a PEM-encoded certificate bundle; it is
// loaded into the client's root CA pool. caCert content is never logged.
func NewSoapClient(u *url.URL, insecure bool, caCert []byte) (*soap.Client, error) {
	sc := soap.NewClient(u, insecure)
	if len(caCert) == 0 {
		return sc, nil
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return nil, errors.New("invalid CA bundle: no valid PEM certificates found")
	}
	// soap.NewClient always initialises TLSClientConfig; SetRootCAs() is not used
	// because it loads from file paths, not in-memory PEM bytes.
	transport := sc.DefaultTransport()
	transport.TLSClientConfig.RootCAs = pool
	return sc, nil
}

// GetVCenterThumbprint retrieves the vCenter certificate using the authenticated
// client's TLS policy, then returns the SHA-1 thumbprint required by VDDK.
// In skip-verification mode this preserves the legacy behavior of accepting the
// presented certificate; otherwise the handshake must verify it successfully.
func GetVCenterThumbprint(ctx context.Context, client *soap.Client) (string, error) {
	u := client.URL()
	if u.Scheme != "https" || u.Hostname() == "" {
		return "", errors.New("vCenter URL must use HTTPS and specify a host")
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	dialer := tls.Dialer{Config: client.DefaultTransport().TLSClientConfig.Clone()}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return "", fmt.Errorf("failed to retrieve vCenter certificate: %w", err)
	}
	defer func() { _ = conn.Close() }()

	certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", errors.New("vCenter did not present a TLS certificate")
	}
	thumbprint := soap.ThumbprintSHA1(certs[0])
	if thumbprint == "" {
		return "", errors.New("SHA-1 certificate thumbprints required by VDDK are unavailable")
	}
	return thumbprint, nil
}
