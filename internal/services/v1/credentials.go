package v1

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/types"
	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
	"github.com/kubev2v/assisted-migration-agent/pkg/vmware"
)

const credentialsRecordID = "credentials"

type CredentialsService struct {
	store  *store.Store
	crypto *crypto.Crypto
	keyMgr *crypto.KeyManager
}

func NewCredentialsService(st *store.Store) *CredentialsService {
	return &CredentialsService{store: st, crypto: crypto.NewCrypto()}
}

func (s *CredentialsService) WithKeyManager(keyMgr *crypto.KeyManager) *CredentialsService {
	s.keyMgr = keyMgr
	return s
}

// SetMasterPassword changes the master password, re-encrypting all stored credentials.
// For initial setup (no password exists yet), pass an empty oldPassword.
func (s *CredentialsService) SetMasterPassword(ctx context.Context, oldPassword, newPassword string) error {
	if newPassword == "" {
		return fmt.Errorf("password not should be empty")
	}

	newHash, err := s.crypto.Hash(newPassword)
	if err != nil {
		return fmt.Errorf("hashing new password: %w", err)
	}

	hasExisting, err := s.HasMasterPassword(ctx)
	if err != nil {
		return err
	}

	if !hasExisting {
		return s.store.Credentials().SavePassword(ctx, newHash)
	}

	ok, err := s.VerifyMasterPassword(ctx, oldPassword)
	if err != nil {
		return fmt.Errorf("verifying old password: %w", err)
	}
	if !ok {
		return fmt.Errorf("old password is incorrect")
	}

	oldKey := s.crypto.Hash256(oldPassword)
	newKey := s.crypto.Hash256(newPassword)

	return s.store.WithTx(ctx, func(txCtx context.Context) error {
		ids, err := s.store.Credentials().List(txCtx)
		if err != nil {
			return fmt.Errorf("listing credentials: %w", err)
		}

		for _, id := range ids {
			encrypted, err := s.store.Credentials().Get(txCtx, id)
			if err != nil {
				return fmt.Errorf("reading credential %s: %w", id, err)
			}

			plain, err := s.crypto.Decrypt(oldKey, encrypted)
			if err != nil {
				return fmt.Errorf("decrypting credential %s: %w", id, err)
			}

			reEncrypted, err := s.crypto.Encrypt(newKey, plain)
			if err != nil {
				return fmt.Errorf("re-encrypting credential %s: %w", id, err)
			}

			if err := s.store.Credentials().Save(txCtx, id, reEncrypted); err != nil {
				return fmt.Errorf("saving credential %s: %w", id, err)
			}
		}

		return s.store.Credentials().SavePassword(txCtx, newHash)
	})
}

func (s *CredentialsService) HasMasterPassword(ctx context.Context) (bool, error) {
	_, err := s.store.Credentials().GetPassword(ctx)
	if srvErrors.IsResourceNotFoundError(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *CredentialsService) VerifyMasterPassword(ctx context.Context, password string) (bool, error) {
	stored, err := s.store.Credentials().GetPassword(ctx)
	if err != nil {
		return false, err
	}

	return s.crypto.Verify(password, stored)
}

func (s *CredentialsService) Store(ctx context.Context, creds models.Credentials) (string, error) {
	normalizedURL, err := vmware.NormalizeAndValidateURL(creds.URL)
	if err != nil {
		return creds.URL, srvErrors.NewValidationError(fmt.Sprintf("invalid vCenter URL: %s", err))
	}

	parsedURL, err := url.Parse(normalizedURL)
	if err != nil {
		return creds.URL, srvErrors.NewValidationError(fmt.Sprintf("invalid vCenter URL: %s", err))
	}
	if parsedURL.User != nil {
		return creds.URL, srvErrors.NewValidationError("vCenter URL must not include embedded credentials")
	}
	parsedURL.RawQuery = ""
	parsedURL.Fragment = ""
	creds.URL = parsedURL.String()

	if s.keyMgr == nil {
		return creds.URL, fmt.Errorf("key manager is not configured")
	}
	if err := vmware.VerifyCredentials(ctx, &creds, "credentials_mgmt"); err != nil {
		if !srvErrors.IsVCenterError(err) {
			return creds.URL, srvErrors.NewVCenterError(err)
		}
		return creds.URL, err
	}
	if err := s.Save(ctx, s.keyMgr.Key(), credentialsRecordID, creds); err != nil {
		return creds.URL, fmt.Errorf("saving credentials: %w", err)
	}

	return creds.URL, nil
}

func (s *CredentialsService) Status(ctx context.Context) (url, username string, err error) {
	creds, err := s.Resolve(ctx)
	if err != nil {
		if srvErrors.IsCredentialsNotSetError(err) {
			return "", "", srvErrors.NewResourceNotFoundError("credentials", credentialsRecordID)
		}
		return "", "", err
	}
	return creds.URL, creds.Username, nil
}

func (s *CredentialsService) Resolve(ctx context.Context) (models.Credentials, error) {
	if s.keyMgr == nil {
		return models.Credentials{}, srvErrors.NewCredentialsNotSetError()
	}
	creds, err := s.Get(ctx, s.keyMgr.Key(), credentialsRecordID)
	if err != nil {
		if srvErrors.IsResourceNotFoundError(err) {
			return models.Credentials{}, srvErrors.NewCredentialsNotSetError()
		}
		return models.Credentials{}, err
	}
	return creds, nil
}

func (s *CredentialsService) GetCapabilities(ctx context.Context) (*models.CapabilityStatus, error) {
	if s.keyMgr == nil {
		return nil, srvErrors.NewResourceNotFoundError("credentials", credentialsRecordID)
	}
	creds, err := s.Get(ctx, s.keyMgr.Key(), credentialsRecordID)
	if err != nil {
		return nil, err
	}

	u, err := url.ParseRequestURI(creds.URL)
	if err != nil {
		return nil, fmt.Errorf("parsing credential URL: %w", err)
	}
	u.User = url.UserPassword(creds.Username, creds.Password)

	connCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	soapClient, err := vmware.NewSoapClient(u, creds.SkipTLS, creds.CACert)
	if err != nil {
		return nil, fmt.Errorf("creating soap client: %w", err)
	}

	vimClient, err := vim25.NewClient(connCtx, soapClient)
	if err != nil {
		return nil, srvErrors.NewVCenterError(fmt.Errorf("connecting to vCenter: %w", err))
	}

	client := &govmomi.Client{
		SessionManager: session.NewManager(vimClient),
		Client:         vimClient,
	}
	if err := client.Login(connCtx, u.User); err != nil {
		return nil, srvErrors.NewVCenterError(err)
	}
	defer func() {
		logoutCtx, logoutCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer logoutCancel()
		_ = client.Logout(logoutCtx)
		client.CloseIdleConnections()
	}()

	dcFolders, err := collectDatacenterFolders(connCtx, vimClient)
	if err != nil {
		return nil, err
	}

	authManager := object.NewAuthorizationManager(vimClient)
	results, err := authManager.FetchUserPrivilegeOnEntities(connCtx, dcFolders.allRefs, creds.Username)
	if err != nil {
		return nil, srvErrors.NewVCenterError(fmt.Errorf("fetching privileges: %w", err))
	}

	grantedPerRef := make(map[types.ManagedObjectReference]map[string]bool, len(results))
	for _, r := range results {
		m := make(map[string]bool, len(r.Privileges))
		for _, p := range r.Privileges {
			m[p] = true
		}
		grantedPerRef[r.Entity] = m
	}

	checkPrivileges := func(refs []types.ManagedObjectReference, required []string) models.OperationCapability {
		missingSet := make(map[string]bool)
		for _, ref := range refs {
			granted := grantedPerRef[ref]
			for _, priv := range required {
				if !granted[priv] {
					missingSet[priv] = true
				}
			}
		}
		if len(missingSet) == 0 {
			return models.OperationCapability{Enabled: true}
		}
		missing := make([]string, 0, len(missingSet))
		for p := range missingSet {
			missing = append(missing, p)
		}
		return models.OperationCapability{Enabled: false, MissingPrivileges: missing}
	}

	status := &models.CapabilityStatus{
		Collector:  checkPrivileges(dcFolders.allRefs, models.CollectorRequiredPrivileges),
		Inspector:  checkPrivileges(dcFolders.vmFolderRefs, models.InspectorRequiredPrivileges),
		Forecaster: checkPrivileges(dcFolders.vmFolderRefs, models.ForecasterRequiredPrivileges),
	}

	zap.S().Named("credentials").Infow("capability check complete",
		"collector", status.Collector.Enabled,
		"inspector", status.Inspector.Enabled,
		"forecaster", status.Forecaster.Enabled,
	)

	return status, nil
}

func (s *CredentialsService) List(ctx context.Context) ([]string, error) {
	return s.store.Credentials().List(ctx)
}

func (s *CredentialsService) Save(ctx context.Context, hash []byte, id string, creds models.Credentials) error {
	if len(hash) == 0 {
		return fmt.Errorf("master password hash cannot be empty")
	}
	encrypted, err := s.crypto.Encrypt(hash, creds)
	if err != nil {
		return fmt.Errorf("encrypting credentials: %w", err)
	}

	return s.store.Credentials().Save(ctx, id, encrypted)
}

func (s *CredentialsService) Get(ctx context.Context, hash []byte, id string) (models.Credentials, error) {
	encrypted, err := s.store.Credentials().Get(ctx, id)
	if err != nil {
		return models.Credentials{}, err
	}

	decrypted, err := s.crypto.Decrypt(hash, encrypted)
	if err != nil {
		return models.Credentials{}, fmt.Errorf("decrypting credentials: %w", err)
	}

	return decrypted, nil
}

func (s *CredentialsService) Delete(ctx context.Context, id string) error {
	return s.store.Credentials().Delete(ctx, id)
}

func (s *CredentialsService) DeleteAll(ctx context.Context) error {
	return s.store.Credentials().DeleteAll(ctx)
}

type datacenterFolders struct {
	allRefs      []types.ManagedObjectReference
	vmFolderRefs []types.ManagedObjectReference
}

func collectDatacenterFolders(ctx context.Context, client *vim25.Client) (*datacenterFolders, error) {
	finder := find.NewFinder(client, false)
	datacenters, err := finder.DatacenterList(ctx, "*")
	if err != nil {
		return nil, srvErrors.NewVCenterError(fmt.Errorf("listing datacenters: %w", err))
	}
	if len(datacenters) == 0 {
		return nil, srvErrors.NewVCenterError(fmt.Errorf("no datacenters found"))
	}

	result := &datacenterFolders{}
	for _, dc := range datacenters {
		folders, err := dc.Folders(ctx)
		if err != nil {
			return nil, srvErrors.NewVCenterError(fmt.Errorf("getting datacenter folders for %s: %w", dc.Name(), err))
		}
		result.allRefs = append(result.allRefs,
			folders.VmFolder.Reference(),
			folders.HostFolder.Reference(),
			folders.DatastoreFolder.Reference(),
			folders.NetworkFolder.Reference(),
		)
		result.vmFolderRefs = append(result.vmFolderRefs, folders.VmFolder.Reference())
	}
	return result, nil
}
