package services

import (
	"context"
	"fmt"
	"net/url"

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
		return creds.URL, err
	}
	if err := s.Save(ctx, s.keyMgr.Key(), credentialsRecordID, creds); err != nil {
		return creds.URL, fmt.Errorf("saving credentials: %w", err)
	}

	return creds.URL, nil
}

func (s *CredentialsService) Status(ctx context.Context) (string, error) {
	url, err := s.GetURL(ctx, credentialsRecordID)
	if err != nil {
		return "", fmt.Errorf("getting credentials: %w", err)
	}
	return url, nil
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

func (s *CredentialsService) GetURL(ctx context.Context, id string) (string, error) {
	return s.store.Credentials().GetURL(ctx, id)
}

func (s *CredentialsService) Delete(ctx context.Context, id string) error {
	return s.store.Credentials().Delete(ctx, id)
}

func (s *CredentialsService) DeleteAll(ctx context.Context) error {
	return s.store.Credentials().DeleteAll(ctx)
}
