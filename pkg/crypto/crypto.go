package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

const (
	hashFormat       = "$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s"
	defaultKeyLength = 32
	defaultSaltSize  = 16
)

// Crypto provides password hashing and field-level encryption.
//
// Hashing uses argon2id with configurable memory, time, threads, and key length.
// Output format: $argon2id$v=VERSION$m=MEMORY,t=TIME,p=THREADS$SALT$HASH
//
// Encryption derives a 32-byte key from the password via argon2id, then encrypts
// each field independently with XChaCha20-Poly1305 using a random 24-byte nonce.
// Each encrypted field is self-contained: base64(salt || nonce || ciphertext).
type Crypto struct {
	saltSize  uint32
	time      uint32
	memory    uint32
	threads   uint8
	keyLength uint32
}

func NewCrypto() *Crypto {
	return &Crypto{
		saltSize:  defaultSaltSize,
		time:      1,
		memory:    64 * 1024, // 64Mb
		threads:   4,
		keyLength: defaultKeyLength,
	}
}

func (c *Crypto) Hash256(password string) []byte {
	h := sha256.Sum256([]byte(password))
	return h[:]
}

func (c *Crypto) Hash(password string) (string, error) {
	salt, err := generateSalt(c.saltSize)
	if err != nil {
		return "", err
	}

	hashRaw := argon2.IDKey(
		[]byte(password),
		salt,
		c.time,
		c.memory,
		c.threads,
		c.keyLength,
	)

	encodedHash := fmt.Sprintf(
		hashFormat,
		argon2.Version,
		c.memory,
		c.time,
		c.threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hashRaw),
	)

	return encodedHash, nil
}

func (c *Crypto) Verify(password, encodedHash string) (bool, error) {
	components := strings.Split(encodedHash, "$")
	if len(components) != 6 {
		return false, errors.New("invalid hash format structure")
	}

	// Validate algorithm identifier
	if components[1] != "argon2id" {
		return false, errors.New("unsupported algorithm variant")
	}

	// Extract version information
	var version int
	if _, err := fmt.Sscanf(components[2], "v=%d", &version); err != nil {
		return false, err
	}

	if version != argon2.Version {
		return false, errors.New("unsupported argon2 version")
	}

	// Parse configuration parameters
	var (
		memory  uint32
		time    uint32
		threads uint8
	)

	if _, err := fmt.Sscanf(components[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, err
	}

	// Decode salt component
	salt, err := base64.RawStdEncoding.DecodeString(components[4])
	if err != nil {
		return false, fmt.Errorf("salt decoding failed: %w", err)
	}

	// Decode hash component
	hash, err := base64.RawStdEncoding.DecodeString(components[5])
	if err != nil {
		return false, fmt.Errorf("hash decoding failed: %w", err)
	}

	keyLength := uint32(len(hash))

	// Generate hash using identical parameters
	computedHash := argon2.IDKey(
		[]byte(password),
		salt,
		time,
		memory,
		threads,
		keyLength,
	)

	return subtle.ConstantTimeCompare(hash, computedHash) == 1, nil
}

func (c *Crypto) Encrypt(hash []byte, creds models.Credentials) (models.Credentials, error) {
	usernameSalt, err := generateSalt(c.saltSize)
	if err != nil {
		return models.Credentials{}, err
	}

	userKey := argon2.IDKey(hash, usernameSalt, c.time, c.memory, c.threads, c.keyLength)

	encUsername, err := encryptField(userKey, usernameSalt, creds.Username)
	if err != nil {
		return models.Credentials{}, err
	}

	pwdSalt, err := generateSalt(c.saltSize)
	if err != nil {
		return models.Credentials{}, err
	}

	pwdKey := argon2.IDKey(hash, pwdSalt, c.time, c.memory, c.threads, c.keyLength)

	encPassword, err := encryptField(pwdKey, pwdSalt, creds.Password)
	if err != nil {
		return models.Credentials{}, err
	}

	return models.Credentials{
		URL:      creds.URL,
		Username: encUsername,
		Password: encPassword,
	}, nil
}

func (c *Crypto) Decrypt(hash []byte, creds models.Credentials) (models.Credentials, error) {
	username, err := decryptField(hash, c, creds.Username)
	if err != nil {
		return models.Credentials{}, fmt.Errorf("decrypting username: %w", err)
	}

	pw, err := decryptField(hash, c, creds.Password)
	if err != nil {
		return models.Credentials{}, fmt.Errorf("decrypting password: %w", err)
	}

	return models.Credentials{
		URL:      creds.URL,
		Username: username,
		Password: pw,
	}, nil
}

func encryptField(key, salt []byte, plaintext string) (string, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	ciphertext := aead.Seal(nil, nonce, []byte(plaintext), nil)

	blob := make([]byte, 0, len(salt)+len(nonce)+len(ciphertext))
	blob = append(blob, salt...)
	blob = append(blob, nonce...)
	blob = append(blob, ciphertext...)

	return base64.RawStdEncoding.EncodeToString(blob), nil
}

func decryptField(hash []byte, c *Crypto, encoded string) (string, error) {
	blob, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}

	saltSize := int(c.saltSize)
	nonceSize := chacha20poly1305.NonceSizeX

	if len(blob) < saltSize+nonceSize {
		return "", errors.New("ciphertext too short")
	}

	salt := blob[:saltSize]
	nonce := blob[saltSize : saltSize+nonceSize]
	ciphertext := blob[saltSize+nonceSize:]

	key := argon2.IDKey(hash, salt, c.time, c.memory, c.threads, c.keyLength)

	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", err
	}

	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed: %w", err)
	}

	return string(plaintext), nil
}

func generateSalt(saltSize uint32) ([]byte, error) {
	salt := make([]byte, saltSize)
	_, err := rand.Read(salt)
	if err != nil {
		return nil, fmt.Errorf("salt generation failed: %w", err)
	}
	return salt, nil
}
