package crypto_test

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/pkg/crypto"
)

var _ = Describe("KeyManager", func() {
	var tmpDir string

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "keymanager-test-*")
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(tmpDir)).To(Succeed())
	})

	It("generates a key file on first run", func() {
		km, err := crypto.NewKeyManager(tmpDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(km.Key()).To(HaveLen(32))

		data, err := os.ReadFile(filepath.Join(tmpDir, "credentials.key"))
		Expect(err).NotTo(HaveOccurred())
		Expect(data).To(Equal(km.Key()))
	})

	It("loads an existing key file", func() {
		km1, err := crypto.NewKeyManager(tmpDir)
		Expect(err).NotTo(HaveOccurred())

		km2, err := crypto.NewKeyManager(tmpDir)
		Expect(err).NotTo(HaveOccurred())

		Expect(km2.Key()).To(Equal(km1.Key()))
	})

	It("sets restrictive file permissions", func() {
		_, err := crypto.NewKeyManager(tmpDir)
		Expect(err).NotTo(HaveOccurred())

		info, err := os.Stat(filepath.Join(tmpDir, "credentials.key"))
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))
	})

	It("returns a defensive copy of the key", func() {
		km, err := crypto.NewKeyManager(tmpDir)
		Expect(err).NotTo(HaveOccurred())

		key1 := km.Key()
		key1[0] ^= 0xff
		Expect(km.Key()).NotTo(Equal(key1))
	})

	It("regenerates on corrupted key file", func() {
		err := os.WriteFile(filepath.Join(tmpDir, "credentials.key"), []byte("short"), 0600)
		Expect(err).NotTo(HaveOccurred())

		km, err := crypto.NewKeyManager(tmpDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(km.Key()).To(HaveLen(32))
	})
})
