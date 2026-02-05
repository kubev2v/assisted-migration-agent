package utils

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"

	. "github.com/kubev2v/assisted-migration-agent/test/e2e/model"
	. "github.com/kubev2v/migration-planner/test/e2e"
)

// GetToken retrieves the private key from the specified path, parses it, and then generates a token
// for the given credentials using the private key. Returns the token or an error.
func GetToken(credentials *User) (string, error) {
	privateKeyString, err := os.ReadFile(PrivateKeyPath)
	if err != nil {
		return "", fmt.Errorf("error, unable to read the private key: %v", err)
	}

	privateKey, err := ParsePrivateKey(string(privateKeyString))
	if err != nil {
		return "", fmt.Errorf("error with parsing the private key: %v", err)
	}

	token, err := GenerateToken(credentials.Username, credentials.Organization, privateKey)
	if err != nil {
		return "",
			fmt.Errorf("error, unable to generate token: %v for user: %s, org: %s",
				err, credentials.Username, credentials.Organization)
	}

	return token, nil
}

// UserAuth returns an auth.User object with the provided username and organization.
func UserAuth(user string, org string, emailDomain string) *User {
	return &User{
		Username:     user,
		Organization: org,
		EmailDomain:  emailDomain,
	}
}

// DefaultUserAuth returns an auth.User object with the default username and organization.
func DefaultUserAuth() *User {
	return UserAuth(DefaultUsername, DefaultOrganization, DefaultEmailDomain)
}

func ParsePrivateKey(content string) (*rsa.PrivateKey, error) {
	// Todo: use the function from migration-planner/internal/cli
	block, _ := pem.Decode([]byte(content))
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return key, nil
}

func GenerateToken(username, organization string, privateKey *rsa.PrivateKey) (string, error) {
	// Todo: use the function from migration-planner/internal/cli
	type TokenClaims struct {
		Username string `json:"username"`
		OrgID    string `json:"org_id"`
		jwt.RegisteredClaims
	}

	// Create claims with multiple fields populated
	claims := TokenClaims{
		username,
		organization,
		jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "test",
			Subject:   "somebody",
			ID:        "1",
			Audience:  []string{"somebody_else"},
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(privateKey)
}
