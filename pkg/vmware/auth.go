package vmware

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
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

// VerifyCredentialsAndPrivileges checks both authentication and vSphere privileges.
// It connects, verifies login, then checks the given privileges on the default VM folder.
func VerifyCredentialsAndPrivileges(ctx context.Context, creds *models.Credentials, requiredPrivileges []string, resourceName string) error {
	u, err := url.ParseRequestURI(creds.URL)
	if err != nil {
		return err
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/sdk"
	}
	u.User = url.UserPassword(creds.Username, creds.Password)

	verifyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	vimClient, err := vim25.NewClient(verifyCtx, soap.NewClient(u, true))
	if err != nil {
		return err
	}

	client := &govmomi.Client{
		SessionManager: session.NewManager(vimClient),
		Client:         vimClient,
	}

	log := zap.S().Named(resourceName)
	log.Info("verifying vCenter credentials")
	if err := client.Login(verifyCtx, u.User); err != nil {
		return srvErrors.NewVCenterError(err)
	}
	defer func() {
		logoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.Logout(logoutCtx)
		client.CloseIdleConnections()
	}()

	log.Info("vCenter credentials verified, checking privileges")

	// Check privileges on the default VM folder (under the datacenter), since
	// vSphere privileges are typically granted at this level rather than root.
	finder := find.NewFinder(vimClient, true)
	dc, err := finder.DefaultDatacenter(verifyCtx)
	if err != nil {
		return srvErrors.NewVCenterError(fmt.Errorf("failed to find datacenter: %w", err))
	}
	finder.SetDatacenter(dc)
	vmFolder, err := finder.DefaultFolder(verifyCtx)
	if err != nil {
		return srvErrors.NewVCenterError(fmt.Errorf("failed to find VM folder: %w", err))
	}
	checkRef := vmFolder.Reference()

	authManager := object.NewAuthorizationManager(vimClient)
	results, err := authManager.FetchUserPrivilegeOnEntities(verifyCtx, []types.ManagedObjectReference{checkRef}, creds.Username)
	if err != nil {
		return srvErrors.NewVCenterError(fmt.Errorf("failed to fetch privileges: %w", err))
	}

	grantedMap := make(map[string]bool)
	if len(results) > 0 {
		for _, p := range results[0].Privileges {
			grantedMap[p] = true
		}
	}

	var missing []string
	for _, req := range requiredPrivileges {
		if !grantedMap[req] {
			missing = append(missing, req)
		}
	}

	if len(missing) > 0 {
		log.Warnw("insufficient privileges on datacenter", "datacenter", dc.Name(), "missing", missing)
		return srvErrors.NewInsufficientPrivilegesError(missing)
	}

	log.Info("vCenter credentials and privileges verified successfully")
	return nil
}

func VerifyCredentials(ctx context.Context, creds *models.Credentials, resourceName string) error {
	u, err := url.ParseRequestURI(creds.URL)
	if err != nil {
		return err
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/sdk"
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

	logoutCtx, logoutCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer logoutCancel()
	_ = client.Logout(logoutCtx)
	client.CloseIdleConnections()

	zap.S().Named(resourceName).Info("vCenter credentials verified successfully")
	return nil
}
