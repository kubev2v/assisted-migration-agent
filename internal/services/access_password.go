package services

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
	srvErrors "github.com/kubev2v/assisted-migration-agent/pkg/errors"
)

const (
	minimumAccessPasswordLength = 8
	maximumAccessPasswordLength = 128
)

type AccessPasswordService struct {
	passwords *store.AccessPasswordStore
	crypto    *crypto.Crypto
}

func NewAccessPasswordService(passwords *store.AccessPasswordStore) *AccessPasswordService {
	return &AccessPasswordService{passwords: passwords, crypto: crypto.NewCrypto()}
}

func (s *AccessPasswordService) Has(ctx context.Context) (bool, error) {
	_, err := s.passwords.Get(ctx)
	if srvErrors.IsResourceNotFoundError(err) {
		return false, nil
	}
	return err == nil, err
}

// Create stores a password once. It returns false when a password already exists.
func (s *AccessPasswordService) Create(ctx context.Context, password string) (bool, error) {
	if err := validateNewPassword(password); err != nil {
		return false, err
	}

	hash, err := s.crypto.Hash(password)
	if err != nil {
		return false, fmt.Errorf("hashing access password: %w", err)
	}
	return s.passwords.Create(ctx, hash)
}

// Replace changes an existing password without affecting encrypted credentials.
func (s *AccessPasswordService) Replace(ctx context.Context, password string) error {
	if err := validateNewPassword(password); err != nil {
		return err
	}

	hash, err := s.crypto.Hash(password)
	if err != nil {
		return fmt.Errorf("hashing access password: %w", err)
	}
	return s.passwords.Replace(ctx, hash)
}

func (s *AccessPasswordService) Verify(ctx context.Context, password string) (bool, error) {
	if err := validateLoginPassword(password); err != nil {
		return false, err
	}

	hash, err := s.passwords.Get(ctx)
	if err != nil {
		return false, err
	}
	return s.crypto.Verify(password, hash)
}

func validateNewPassword(password string) error {
	if err := validateLoginPassword(password); err != nil {
		return err
	}
	if utf8.RuneCountInString(password) < minimumAccessPasswordLength {
		return fmt.Errorf("password must contain at least %d characters", minimumAccessPasswordLength)
	}
	return nil
}

func validateLoginPassword(password string) error {
	if !utf8.ValidString(password) {
		return fmt.Errorf("password must be valid UTF-8")
	}
	length := utf8.RuneCountInString(password)
	if length == 0 || length > maximumAccessPasswordLength {
		return fmt.Errorf("password must contain between 1 and %d characters", maximumAccessPasswordLength)
	}
	return nil
}
