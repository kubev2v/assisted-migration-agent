package cmd

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/duckdb/duckdb-go/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kubev2v/assisted-migration-agent/internal/store"
	"github.com/kubev2v/assisted-migration-agent/internal/store/migrations"
	"github.com/kubev2v/assisted-migration-agent/internal/upgrade"
)

var _ = Describe("Offline saved-data validation CLI", func() {
	It("checks real stores without application configuration, cleanup, or asset creation", func() {
		folder := GinkgoT().TempDir()
		oldImage := "sha256:" + strings.Repeat("a", 64)
		newImage := "sha256:" + strings.Repeat("b", 64)
		db, err := sql.Open("duckdb", filepath.Join(folder, "agent.duckdb"))
		Expect(err).NotTo(HaveOccurred())
		Expect(migrations.RunMain(context.Background(), db)).To(Succeed())
		_, err = db.Exec(`INSERT INTO collections("database",state) VALUES ('collection_99','running')`)
		Expect(err).NotTo(HaveOccurred())
		Expect(db.Close()).To(Succeed())
		snapshot, err := store.ReadUpgradeSnapshot(context.Background(), folder)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.MkdirAll(filepath.Join(folder, "updates/validation"), 0700)).To(Succeed())
		data, err := json.Marshal(upgrade.Baseline{ContractVersion: 1, AttemptID: "cli-test", PreviousImage: oldImage, CandidateImage: newImage, Snapshot: snapshot})
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(folder, "updates/validation/cli-test.json"), data, 0600)).To(Succeed())
		recovery, err := migrations.NewRecovery(folder)
		Expect(err).NotTo(HaveOccurred())
		_, err = recovery.Run(context.Background(), migrations.Request{Operation: "prepare", AttemptID: "cli-test", PreviousImage: oldImage, CandidateImage: newImage})
		Expect(err).NotTo(HaveOccurred())
		before, err := os.ReadFile(filepath.Join(folder, "agent.duckdb"))
		Expect(err).NotTo(HaveOccurred())
		for _, image := range []string{newImage, "sha256:" + strings.Repeat("c", 64)} {
			root := NewRootCommand("test", "test", "test")
			var stdout bytes.Buffer
			root.SetOut(&stdout)
			root.SetArgs([]string{"upgrade", "validate", "--data-folder", folder, "--attempt-id", "cli-test", "--image-id", image, "--log-level", "invalid"})
			err := root.Execute()
			var response struct {
				Valid     bool   `json:"valid"`
				Error     string `json:"error"`
				AttemptID string `json:"attempt_id"`
			}
			Expect(json.Unmarshal(stdout.Bytes(), &response)).To(Succeed())
			Expect(response.AttemptID).To(Equal("cli-test"))
			if image == newImage {
				Expect(err).NotTo(HaveOccurred())
				Expect(response.Valid).To(BeTrue())
			} else {
				Expect(err).To(HaveOccurred())
				Expect(response.Valid).To(BeFalse())
				Expect(response.Error).NotTo(BeEmpty())
			}
		}
		after, err := os.ReadFile(filepath.Join(folder, "agent.duckdb"))
		Expect(err).NotTo(HaveOccurred())
		Expect(after).To(Equal(before))
		for _, name := range []string{"credentials.key", "updates/control-token", "updates/authorization.json"} {
			_, err := os.Stat(filepath.Join(folder, name))
			Expect(os.IsNotExist(err)).To(BeTrue())
		}
	})
})
