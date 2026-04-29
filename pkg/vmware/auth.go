package vmware

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/vim25/soap"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"

	"github.com/vmware/govmomi/vim25"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
)

// ValidateUserPrivilegesOnEntity checks whether the specified user has all the required privileges
// on a given vSphere entity (e.g., VM, folder, datacenter).
func ValidateUserPrivilegesOnEntity(
	ctx context.Context,
	client *vim25.Client,
	ref types.ManagedObjectReference,
	requiredPrivileges []string,
	username string,
) error {
	authManager := object.NewAuthorizationManager(client)

	results, err := authManager.FetchUserPrivilegeOnEntities(ctx, []types.ManagedObjectReference{ref}, username)
	if err != nil {
		return fmt.Errorf("failed to fetch user privileges: %w", err)
	}

	if len(results) == 0 {
		return fmt.Errorf("no privileges returned for user %s", username)
	}

	grantedMap := make(map[string]bool)
	for _, p := range results[0].Privileges {
		grantedMap[p] = true
	}

	var missing []string
	for _, req := range requiredPrivileges {
		if !grantedMap[req] {
			missing = append(missing, req)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("user %s is missing required privileges: %v", username, missing)
	}

	return nil
}

func (m *VMManager) ValidatePrivileges(ctx context.Context, moid string, requiredPrivileges []string) error {
	return ValidateUserPrivilegesOnEntity(ctx, m.gc.Client, refFromMoid(moid), requiredPrivileges, m.username)
}

func VerifyCredentials(ctx context.Context, creds *models.Credentials, resourceName string) error {
	u, err := url.ParseRequestURI(creds.URL)
	if err != nil {
		return err
	}

	u.User = url.UserPassword(creds.Username, creds.Password)

	verifyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	vimClient, err := vim25.NewClient(verifyCtx, soap.NewClient(u, true))
	if err != nil {
		return err
	}

	client := &govmomi.Client{
		SessionManager: session.NewManager(vimClient),
		Client:         vimClient,
	}

	zap.S().Named(resourceName).Info("verifying vCenter credentials")
	if err := client.Login(verifyCtx, u.User); err != nil {
		return srvErrors.NewVCenterError(err)
	}

	_ = client.Logout(verifyCtx)
	client.CloseIdleConnections()

	zap.S().Named(resourceName).Info("vCenter credentials verified successfully")
	return nil
}

func EnsureSdkSuffix(vUrl string) (string, error) {
	u, err := url.Parse(vUrl)
	if err != nil {
		return "", err
	}

	path := strings.TrimRight(u.Path, "/")

	if !strings.HasSuffix(path, "/sdk") {
		path += "/sdk"
	}

	u.Path = path
	return u.String(), nil
}
