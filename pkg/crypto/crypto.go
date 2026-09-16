package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"

	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

const (
	hashFormat       = "$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s"
	defaultKeyLength = 32
	defaultSaltSize  = 16
	maxHashMemory    = 64 * 1024
	maxHashTime      = 3
	maxHashThreads   = 4
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

	version, err := parseUintParameter(components[2], "v", uint64(argon2.Version))
	if err != nil {
		return false, err
	}

	if version != uint64(argon2.Version) {
		return false, errors.New("unsupported argon2 version")
	}

	memory, time, threads, err := parseHashParameters(components[3])
	if err != nil {
		return false, err
	}

	// Decode salt component
	salt, err := base64.RawStdEncoding.DecodeString(components[4])
	if err != nil {
		return false, fmt.Errorf("salt decoding failed: %w", err)
	}
	if len(salt) != int(c.saltSize) {
		return false, errors.New("unexpected salt length")
	}

	// Decode hash component
	hash, err := base64.RawStdEncoding.DecodeString(components[5])
	if err != nil {
		return false, fmt.Errorf("hash decoding failed: %w", err)
	}
	if len(hash) != int(c.keyLength) {
		return false, errors.New("unexpected hash length")
	}

	// Generate hash using identical parameters
	computedHash := argon2.IDKey(
		[]byte(password),
		salt,
		time,
		memory,
		threads,
		c.keyLength,
	)

	return subtle.ConstantTimeCompare(hash, computedHash) == 1, nil
}

func parseUintParameter(parameter, name string, max uint64) (uint64, error) {
	prefix := name + "="
	if !strings.HasPrefix(parameter, prefix) {
		return 0, fmt.Errorf("invalid %s parameter", name)
	}
	value, err := strconv.ParseUint(strings.TrimPrefix(parameter, prefix), 10, 64)
	if err != nil || value > max {
		return 0, fmt.Errorf("invalid %s parameter", name)
	}
	return value, nil
}

func parseHashParameters(encoded string) (uint32, uint32, uint8, error) {
	parameters := strings.Split(encoded, ",")
	if len(parameters) != 3 {
		return 0, 0, 0, errors.New("invalid hash parameters")
	}
	memory, err := parseUintParameter(parameters[0], "m", maxHashMemory)
	if err != nil || memory == 0 {
		return 0, 0, 0, errors.New("invalid hash memory")
	}
	time, err := parseUintParameter(parameters[1], "t", maxHashTime)
	if err != nil || time == 0 {
		return 0, 0, 0, errors.New("invalid hash time")
	}
	threads, err := parseUintParameter(parameters[2], "p", maxHashThreads)
	if err != nil || threads == 0 || memory < 8*threads {
		return 0, 0, 0, errors.New("invalid hash threads")
	}
	return uint32(memory), uint32(time), uint8(threads), nil
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
		SkipTLS:  creds.SkipTLS,
		CACert:   creds.CACert,
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
		SkipTLS:  creds.SkipTLS,
		CACert:   creds.CACert,
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
