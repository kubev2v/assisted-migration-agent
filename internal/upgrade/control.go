// Package upgrade drains running work and validates saved data for host upgrades.
package upgrade

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"regexp"
)

const ContractVersion = 1

var attemptPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
var imagePattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// loadControlToken protects the drain endpoint from other appliance-network clients.
// Offline validation does not need or create this token.
func loadControlToken(folder string) (string, error) {
	root, err := os.OpenRoot(folder)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	if err := root.MkdirAll("updates", 0700); err != nil {
		return "", err
	}
	file, err := root.OpenFile("updates/control-token", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	if err == nil {
		data := make([]byte, 32)
		if _, err := rand.Read(data); err != nil {
			return "", errors.Join(err, file.Close())
		}
		_, writeErr := file.WriteString(hex.EncodeToString(data))
		if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
			return "", err
		}
	}
	data, err := root.ReadFile("updates/control-token")
	if err != nil {
		return "", err
	}
	decoded, err := hex.DecodeString(string(data))
	if err != nil || len(decoded) != 32 {
		return "", fmt.Errorf("invalid upgrade control token")
	}
	return string(data), nil
}
