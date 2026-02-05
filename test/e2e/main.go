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
	BackendUserEndpoint  string
	AgentProxyUrl        string
	AgentAPIUrl          string
	AgentImage           string
	PodmanSocket         string
	KeepContainers       bool
	IsoPath              string
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
	flag.StringVar(&cfg.BackendAgentEndpoint, "backend-agent-endpoint", "http://localhost:7443", "Agent endpoint on backend (port 7443)")
	flag.StringVar(&cfg.BackendUserEndpoint, "backend-user-endpoint", "http://localhost:3443", "User endpoint on backend (port 3443)")
	flag.StringVar(&cfg.AgentProxyUrl, "agent-proxy-url", "http://localhost:8080", "Agent proxy url")
	flag.StringVar(&cfg.AgentAPIUrl, "agent-api-url", "https://localhost:8000", "Agent local API url")
	flag.StringVar(&cfg.PodmanSocket, "podman-socket", "unix:///run/user/1000/podman/podman.sock", "Podman socket path")
	flag.StringVar(&cfg.IsoPath, "iso-path", "", "Path to directory containing rhcos-live-iso.x86_64.iso")
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
