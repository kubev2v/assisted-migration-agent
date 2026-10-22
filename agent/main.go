package main

import (
	"fmt"
	"os"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/agent/cmd"
)

// These are set at build time via -ldflags
var (
	version     = "v0.0.0"  // Set via -ldflags "-X main.version=..."
	gitCommit   = "unknown" // Set via -ldflags "-X main.gitCommit=..."
	uiGitCommit = "unknown" // Set via -ldflags "-X main.uiGitCommit=..."
)

func main() {
	root := cmd.NewRootCommand(version, gitCommit, uiGitCommit)
	err := root.Execute()
	_ = zap.L().Sync()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
