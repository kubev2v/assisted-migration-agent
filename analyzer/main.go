package main

import (
	"fmt"
	"os"

	"go.uber.org/zap"

	"github.com/kubev2v/assisted-migration-agent/analyzer/cmd"
)

var (
	version   = "v0.0.0"
	gitCommit = "unknown"
)

func main() {
	root := cmd.NewRootCommand(version, gitCommit)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = zap.L().Sync()
}
