package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/zap"
)

type configuration struct {
	BackendImage         string
	BackendAgentEndpoint string
	AgentProxyUrl        string
	AgentAPIUrl          string
	AgentImage           string
	Database             string
	PodmanSocket         string
	KeepContainers       bool
}

var (
	cfg    configuration
	runner *PodmanRunner
)

func (c configuration) Validate() error {
	if c.BackendImage == "" {
		return errors.New("backend container image is empty")
	}
	if c.AgentImage == "" {
		return errors.New("agent container image is empty")
	}
	if c.Database == "" {
		return errors.New("database url is empty")
	}
	if _, err := url.Parse(c.BackendAgentEndpoint); err != nil {
		return fmt.Errorf("failed to parse agent endpoint: %v", err)
	}
	if _, err := url.Parse(c.AgentProxyUrl); err != nil {
		return fmt.Errorf("failed to parse agent proxy url: %v", err)
	}
	return nil
}

func main() {
	flag.StringVar(&cfg.AgentImage, "agent-image", "", "Agent container image")
	flag.StringVar(&cfg.BackendImage, "backend-image", "", "Backend container image")
	flag.StringVar(&cfg.BackendAgentEndpoint, "backend-agent-endpoint", "http://localhost:7443", "Agent endpoint on backend")
	flag.StringVar(&cfg.AgentProxyUrl, "agent-proxy-url", "http://localhost:8080", "Agent proxy url")
	flag.StringVar(&cfg.AgentAPIUrl, "agent-api-url", "https://localhost:8000", "Agent local API url")
	flag.StringVar(&cfg.Database, "db-url", "postgresql://planner:adminpass@localhost:5432/planner", "Database url like postgresql://user:secret@localhost/dbname")
	flag.StringVar(&cfg.PodmanSocket, "podman-socket", "unix:///run/user/1000/podman/podman.sock", "Podman socket path")
	flag.BoolVar(&cfg.KeepContainers, "keep-containers", false, "Keep containers running after test completion (useful for debugging)")
	flag.Parse()

	logger, err := zap.NewDevelopment()
	if err != nil {
		log.Fatalf("failed to initialize logger: %v", err)
	}
	zap.ReplaceGlobals(logger)
	defer logger.Sync()

	if err := cfg.Validate(); err != nil {
		log.Fatalf("failed to validate configuration: %v", err)
	}

	RegisterFailHandler(Fail)
	if !RunSpecs(&testing.T{}, "E2E Suite") {
		os.Exit(1)
	}
}
