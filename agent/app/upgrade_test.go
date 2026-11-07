package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/config"
	"github.com/kubev2v/assisted-migration-agent/internal/datalock"
	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/internal/upgrade"
)

func TestProbationBootstrap(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Probation bootstrap")
}

var _ = Describe("Probation startup", func() {
	It("rejects missing normal identities before creating writable stores", func() {
		folder := GinkgoT().TempDir()
		cfg := config.NewConfigurationWithOptionsAndDefaults(config.WithAgent(config.Agent{DataFolder: folder}))
		_, _, _, err := initV2Managed(cfg, upgrade.Options{})
		Expect(err).To(MatchError(ContainSubstring("invalid agent-id")))
		files, err := os.ReadDir(folder)
		Expect(err).NotTo(HaveOccurred())
		Expect(files).To(BeEmpty())
	})
	It("bypasses normal bootstrap, preserves stale markers, and never creates a credential key", func() {
		folder := GinkgoT().TempDir()
		oldImage := "sha256:" + strings.Repeat("a", 64)
		candidate := "sha256:" + strings.Repeat("b", 64)
		db, err := sql.Open("duckdb", filepath.Join(folder, "agent.duckdb"))
		Expect(err).NotTo(HaveOccurred())
		Expect(migrations.RunMain(context.Background(), db)).To(Succeed())
		_, err = db.Exec(`INSERT INTO collections("database",state) VALUES ('collection_99','running')`)
		Expect(err).NotTo(HaveOccurred())
		Expect(db.Close()).To(Succeed())
		_, err = upgrade.LoadToken(folder, false)
		Expect(err).NotTo(HaveOccurred())
		snapshot, err := store.ReadUpgradeSnapshot(context.Background(), folder)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.MkdirAll(filepath.Join(folder, "updates/validation"), 0700)).To(Succeed())
		baseline := upgrade.Baseline{ContractVersion: 1, AttemptID: "bootstrap-test", PreviousImage: oldImage, CandidateImage: candidate, Snapshot: snapshot}
		data, err := json.Marshal(baseline)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(folder, "updates/validation/bootstrap-test.json"), data, 0600)).To(Succeed())
		recovery, err := migrations.NewRecovery(folder)
		Expect(err).NotTo(HaveOccurred())
		_, err = recovery.Run(context.Background(), migrations.Request{Operation: "prepare", AttemptID: baseline.AttemptID, PreviousImage: oldImage, CandidateImage: candidate})
		Expect(err).NotTo(HaveOccurred())
		auth := upgrade.Authorization{ContractVersion: 1, AttemptID: baseline.AttemptID, Phase: "validating", PreviousImage: oldImage, CandidateImage: candidate}
		data, err = json.Marshal(auth)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(folder, "updates/authorization.json"), data, 0600)).To(Succeed())
		before, err := os.ReadFile(filepath.Join(folder, "agent.duckdb"))
		Expect(err).NotTo(HaveOccurred())
		cfg := config.NewConfigurationWithOptionsAndDefaults(config.WithAgent(config.Agent{DataFolder: folder, OpaPoliciesFolder: filepath.Join(folder, "missing-policies")}), config.WithAuth(config.Authentication{Enabled: true, JWTFilePath: filepath.Join(folder, "missing-jwt")}))
		_, cleanup, lifecycle, err := initV2Managed(cfg, upgrade.Options{AttemptID: baseline.AttemptID, ImageID: candidate})
		Expect(err).NotTo(HaveOccurred())
		defer cleanup()
		defer lifecycle.Close()
		Expect(lifecycle.Status().State).To(Equal("ready"))
		after, err := os.ReadFile(filepath.Join(folder, "agent.duckdb"))
		Expect(err).NotTo(HaveOccurred())
		Expect(after).To(Equal(before))
		_, err = os.Stat(filepath.Join(folder, "credentials.key"))
		Expect(os.IsNotExist(err)).To(BeTrue())
	})
	It("refuses application startup when another data user holds the directory", func() {
		folder := GinkgoT().TempDir()
		lock, err := datalock.Acquire(folder)
		Expect(err).NotTo(HaveOccurred())
		defer func() { Expect(lock.Close()).To(Succeed()) }()
		cfg := config.NewConfigurationWithOptionsAndDefaults(config.WithAgent(config.Agent{DataFolder: folder}))
		Expect(Run(cfg)).To(MatchError(ContainSubstring("busy")))
	})
})
