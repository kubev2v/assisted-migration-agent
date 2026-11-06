package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Upgrade capability probe", func() {
	It("returns one JSON document without application configuration or logger output", func() {
		root := NewRootCommand("test", "test", "test")
		var stdout bytes.Buffer
		root.SetOut(&stdout)
		root.SetArgs([]string{"upgrade", "capabilities", "--log-level", "invalid"})
		Expect(root.Execute()).To(Succeed())
		var response struct {
			ContractVersion int      `json:"contract_version"`
			Features        []string `json:"features"`
		}
		Expect(json.Unmarshal(stdout.Bytes(), &response)).To(Succeed())
		Expect(response.ContractVersion).To(Equal(1))
		Expect(response.Features).To(ContainElements("maintenance", "probation", "data-lock"))
	})
})

var _ = Describe("Appliance entrypoint", func() {
	DescribeTable("guards persistent extension writes", func(args []string, managed, envProbation, copyExpected bool) {
		folder := GinkgoT().TempDir()
		source, err := os.ReadFile("../entrypoint.sh")
		Expect(err).NotTo(HaveOccurred())
		app := filepath.Join(folder, "app")
		data := filepath.Join(folder, "data")
		Expect(os.MkdirAll(filepath.Join(app, "extensions"), 0700)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(data, "updates"), 0700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(app, "extensions/test.duckdb_extension"), []byte("extension"), 0600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(app, "agent"), []byte("#!/bin/sh\nexit 0\n"), 0700)).To(Succeed())
		if managed {
			Expect(os.WriteFile(filepath.Join(data, "updates/authorization.json"), []byte("{}"), 0600)).To(Succeed())
		}
		script := strings.ReplaceAll(string(source), "/app", app)
		script = strings.ReplaceAll(script, "/var/lib/agent", data)
		scriptPath := filepath.Join(folder, "entrypoint.sh")
		Expect(os.WriteFile(scriptPath, []byte(script), 0700)).To(Succeed())
		command := exec.Command("/bin/sh", append([]string{scriptPath}, args...)...)
		command.Env = append(os.Environ(), "AGENT_UPGRADE_ATTEMPT_ID=")
		if envProbation {
			command.Env = append(command.Env, "AGENT_UPGRADE_ATTEMPT_ID=attempt")
		}
		output, err := command.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), string(output))
		_, err = os.Stat(filepath.Join(data, "test.duckdb_extension"))
		Expect(err == nil).To(Equal(copyExpected))
	}, Entry("standalone boot", []string{"run"}, false, false, true), Entry("capability probe", []string{"upgrade", "capabilities"}, false, false, false), Entry("offline CLI", []string{"migrate", "prepare"}, false, false, false), Entry("probation flag", []string{"run", "--upgrade-attempt-id", "attempt"}, false, false, false), Entry("probation equals flag", []string{"run", "--upgrade-attempt-id=attempt"}, false, false, false), Entry("probation environment", []string{"run"}, false, true, false), Entry("unresolved or committed host decision", []string{"run"}, true, false, false))
})
