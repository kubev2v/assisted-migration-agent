package crypto

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.uber.org/zap"
)

const (
	keyFileName = "credentials.key"
	keySize     = 32
)

type KeyManager struct {
	key []byte
}

func NewKeyManager(dataFolder string) (*KeyManager, error) {
	keyPath := filepath.Join(dataFolder, keyFileName)

	data, err := os.ReadFile(keyPath)
	if err == nil {
		if len(data) == keySize {
			return &KeyManager{key: data}, nil
		}
		zap.S().Warnf("corrupted key file %s (%d bytes, expected %d) — regenerating; previously encrypted credentials are unrecoverable", keyPath, len(data), keySize)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("reading key file: %w", err)
	}

	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating encryption key: %w", err)
	}

	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		return nil, fmt.Errorf("writing encryption key: %w", err)
	}

	return &KeyManager{key: key}, nil
}

func (km *KeyManager) Key() []byte {
	out := make([]byte, len(km.key))
	copy(out, km.key)
	return out
}
