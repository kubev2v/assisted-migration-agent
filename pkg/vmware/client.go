package vmware

import (
	"context"
	"fmt"
	"net/url"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/soap"
)

// NewVsphereClient creates and authenticates a new vSphere client connection to a vCenter server.
//
// Parameters:
//   - ctx: the context for the API request.
//   - vcenterUrl: the URL of the vCenter server (e.g., "https://vcenter.example.com/sdk").
//   - username: the username for authentication.
//   - password: the password for authentication.
//   - insecure: if true, skips TLS certificate verification (use only for testing).
//
// Returns an error if:
//   - the vCenter URL cannot be parsed,
//   - the vim25 client creation fails,
//   - or authentication to vCenter fails.
func NewVsphereClient(ctx context.Context, vcenterUrl, username, password string, insecure bool) (*govmomi.Client, error) {
	u, err := soap.ParseURL(vcenterUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse vCenter URL: %w", err)
	}

	u.User = url.UserPassword(username, password)

	soapClient := soap.NewClient(u, insecure)

	vimClient, err := vim25.NewClient(ctx, soapClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create vim25 client: %w", err)
	}

	client := &govmomi.Client{
		Client:         vimClient,
		SessionManager: session.NewManager(vimClient),
	}

	if err := client.Login(ctx, u.User); err != nil {
		return nil, fmt.Errorf("failed to login to vCenter: %w", err)
	}

	return client, nil
}
