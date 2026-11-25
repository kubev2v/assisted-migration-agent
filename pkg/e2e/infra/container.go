package infra

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"go.podman.io/podman/v6/pkg/bindings/containers"

	"github.com/kubev2v/assisted-migration-agent/pkg/e2e/vcsim"
)

const (
	dbContainerName      = "test-planner-db"
	agentContainerName   = "test-planner-agent"
	backendContainerName = "test-planner"
	vcsimContainerName   = "test-vcsim"
	vcsimImage           = "docker.io/vmware/vcsim:latest"
	vcsimPort            = 8989
	agentVolumeName      = "test-agent-data"

	// Database configuration
	dbType     = "pgsql"
	dbHost     = "localhost"
	dbPort     = "5432"
	dbName     = "planner"
	dbUser     = "planner"
	dbPassword = "adminpass"
)

// ContainerInfraManager implements InfraManager using Podman containers.
type ContainerInfraManager struct {
	runner        *PodmanRunner
	backendImage  string
	agentImage    string
	isoPath       string
	oidc          *OIDCServer
	vcsimDataset  *vcsim.Dataset
	vcsimModelDir string
}

// NewContainerInfraManager creates a new ContainerInfraManager.
func NewContainerInfraManager(podmanSocket, backendImage, agentImage, isoPath string) (*ContainerInfraManager, error) {
	runner, err := NewPodmanRunner(podmanSocket)
	if err != nil {
		return nil, err
	}
	return &ContainerInfraManager{
		runner:       runner,
		backendImage: backendImage,
		agentImage:   agentImage,
		isoPath:      isoPath,
	}, nil
}

func (c *ContainerInfraManager) StartOIDC(addr string) error {
	oidc, err := NewOIDCServer(addr)
	if err != nil {
		return err
	}
	c.oidc = oidc
	return nil
}

func (c *ContainerInfraManager) StopOIDC() error {
	if c.oidc != nil {
		return c.oidc.Stop()
	}
	return nil
}

func (c *ContainerInfraManager) GenerateToken(username, orgID, email string) (string, error) {
	if c.oidc == nil {
		return "", fmt.Errorf("OIDC server not started")
	}
	return c.oidc.GenerateToken(username, orgID, email)
}

func (c *ContainerInfraManager) StartPostgres() error {
	_, err := c.runner.StartContainer(
		NewContainerConfig(dbContainerName, "docker.io/library/postgres:17").
			WithPort(5432, 5432).
			WithEnvVar("POSTGRES_USER", dbUser).
			WithEnvVar("POSTGRES_PASSWORD", dbPassword).
			WithEnvVar("POSTGRES_DB", dbName),
	)
	return err
}

func (c *ContainerInfraManager) StopPostgres() error {
	if err := c.runner.StopContainer(dbContainerName); err != nil {
		return err
	}
	return c.runner.RemoveContainer(dbContainerName)
}

func (c *ContainerInfraManager) StartBackend() error {
	cfg := NewContainerConfig(backendContainerName, c.backendImage).
		WithPort(7443, 7443).
		WithPort(3443, 3443).
		WithEnvVar("DB_TYPE", dbType).
		WithEnvVar("DB_HOST", dbHost).
		WithEnvVar("DB_PORT", dbPort).
		WithEnvVar("DB_NAME", dbName).
		WithEnvVar("DB_USER", dbUser).
		WithEnvVar("DB_PASS", dbPassword).
		WithEnvVar("MIGRATION_PLANNER_MIGRATIONS_FOLDER", "/app/migrations").
		WithEnvVar("MIGRATION_PLANNER_AGENT_AUTH_ENABLED", "false").
		WithEnvVar("MIGRATION_PLANNER_LOG_LEVEL", "debug")

	if c.oidc != nil {
		cfg = cfg.
			WithEnvVar("MIGRATION_PLANNER_AUTH", "rhsso").
			WithEnvVar("MIGRATION_PLANNER_JWK_URL", c.oidc.JWKSURL())
	} else {
		cfg = cfg.WithEnvVar("MIGRATION_PLANNER_AUTH", "none")
	}

	if c.isoPath != "" {
		cfg = cfg.WithBindMount(c.isoPath, "/iso").
			WithEnvVar("MIGRATION_PLANNER_ISO_PATH", "/iso/rhcos-live-iso.x86_64.iso")
	}

	_, err := c.runner.StartContainer(cfg)
	return err
}

func (c *ContainerInfraManager) StopBackend() error {
	if err := c.runner.StopContainer(backendContainerName); err != nil {
		return err
	}
	return c.runner.RemoveContainer(backendContainerName)
}

// StartVcsim generates a vcsim model seeded with the standard 50-VM
// inventory (vcsim.DefaultInventory) and starts vcsim loaded from it. The
// inventory can be grown or shrunk at runtime via AddVMs/RemoveVM, which
// regenerate the model and restart the container.
func (c *ContainerInfraManager) StartVcsim() error {
	modelDir, err := os.MkdirTemp("", "vcsim-model-*")
	if err != nil {
		return fmt.Errorf("creating vcsim model dir: %w", err)
	}

	gen, err := vcsim.NewDataset(modelDir)
	if err != nil {
		return fmt.Errorf("creating vcsim dataset: %w", err)
	}
	gen.AddVMs(vcsim.DefaultInventory())
	if err := gen.GenerateXML(); err != nil {
		return fmt.Errorf("generating vcsim model: %w", err)
	}

	c.vcsimDataset = gen
	c.vcsimModelDir = modelDir

	if _, err := c.runner.StartContainer(c.vcsimContainerConfig()); err != nil {
		return fmt.Errorf("starting vcsim container: %w", err)
	}
	return waitReady(VcsimURL, 30*time.Second)
}

func (c *ContainerInfraManager) StopVcsim() error {
	if err := c.runner.StopContainer(vcsimContainerName); err != nil {
		return err
	}
	if err := c.runner.RemoveContainer(vcsimContainerName); err != nil {
		return err
	}
	if c.vcsimModelDir != "" {
		_ = os.RemoveAll(c.vcsimModelDir)
	}
	c.vcsimDataset = nil
	c.vcsimModelDir = ""
	return nil
}

func (c *ContainerInfraManager) AddVMs(vms []vcsim.VM) ([]vcsim.VM, error) {
	if c.vcsimDataset == nil {
		return nil, errors.New("vcsim not started")
	}
	created := c.vcsimDataset.AddVMs(vms)
	if err := c.regenerateAndRestartVcsim(); err != nil {
		return nil, err
	}
	return created, nil
}

func (c *ContainerInfraManager) RemoveVM(name string) error {
	if c.vcsimDataset == nil {
		return errors.New("vcsim not started")
	}
	if !c.vcsimDataset.RemoveVM(name) {
		return fmt.Errorf("VM %q not found", name)
	}
	return c.regenerateAndRestartVcsim()
}

func (c *ContainerInfraManager) regenerateAndRestartVcsim() error {
	if err := c.vcsimDataset.GenerateXML(); err != nil {
		return fmt.Errorf("generating vcsim model: %w", err)
	}
	if err := c.runner.StopContainer(vcsimContainerName); err != nil {
		return fmt.Errorf("stopping vcsim: %w", err)
	}
	if err := c.runner.RemoveContainer(vcsimContainerName); err != nil {
		return fmt.Errorf("removing vcsim: %w", err)
	}
	// Wait for the port to be released by the container runtime's networking stack.
	time.Sleep(2 * time.Second)
	if _, err := c.runner.StartContainer(c.vcsimContainerConfig()); err != nil {
		return fmt.Errorf("restarting vcsim: %w", err)
	}
	return waitReady(VcsimURL, 30*time.Second)
}

func (c *ContainerInfraManager) vcsimContainerConfig() *ContainerConfig {
	return NewContainerConfig(vcsimContainerName, vcsimImage).
		WithPort(vcsimPort, vcsimPort).
		WithBindMount(c.vcsimModelDir, "/model").
		WithCmd(
			"-l", fmt.Sprintf(":%d", vcsimPort),
			"-username", VcsimUsername,
			"-password", VcsimPassword,
			"-load", "/model",
		)
}

func (c *ContainerInfraManager) StartAgent(cfg AgentConfig) (string, error) {
	updateInterval := cfg.UpdateInterval
	if updateInterval == "" {
		updateInterval = "1s"
	}

	containerCfg := NewContainerConfig(agentContainerName, c.agentImage).
		WithPort(8000, 8000).
		WithVolume(agentVolumeName, "/var/lib/agent").
		WithEnvVar("AGENT_SERVER_MODE", "prod").
		WithEnvVar("AGENT_SERVER_STATICS_FOLDER", "/app/static").
		WithEnvVar("AGENT_OPA_POLICIES_FOLDER", "/app/policies").
		WithEnvVar("AGENT_MODE", cfg.Mode).
		WithEnvVar("AGENT_AGENT_ID", cfg.AgentID).
		WithEnvVar("AGENT_SOURCE_ID", cfg.SourceID).
		WithEnvVar("AGENT_DATA_FOLDER", "/var/lib/agent").
		WithEnvVar("AGENT_CONSOLE_URL", cfg.ConsoleURL).
		WithEnvVar("AGENT_CONSOLE_UPDATE_INTERVAL", updateInterval)

	if cfg.APIVersion != "" {
		containerCfg.WithEnvVar("AGENT_API", cfg.APIVersion)
	}
	if cfg.Version != "" {
		containerCfg.WithCmd("run", "--version", cfg.Version)
	}

	return c.runner.StartContainer(containerCfg)
}

func (c *ContainerInfraManager) StopAgent() error {
	return c.runner.StopContainer(agentContainerName)
}

func (c *ContainerInfraManager) RestartAgent() error {
	return c.runner.RestartContainer(agentContainerName)
}

func (c *ContainerInfraManager) RemoveAgent() error {
	_ = c.runner.StopContainer(agentContainerName)
	_ = c.runner.RemoveContainer(agentContainerName)
	return c.runner.RemoveVolume(agentVolumeName)
}

func (c *ContainerInfraManager) CopyAgentFiles(path string, destination io.Writer) error {
	copyFiles, err := containers.CopyToArchive(c.runner.conn, agentContainerName, path, destination)
	if err != nil {
		return fmt.Errorf("copying agent files: %w", err)
	}
	return copyFiles()
}

// waitReady polls url until it responds or timeout elapses.
func waitReady(url string, timeout time.Duration) error {
	client := &http.Client{
		Timeout: 2 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("vcsim did not become ready within %s", timeout)
		case <-ticker.C:
			resp, err := client.Get(url)
			if err != nil {
				continue
			}
			_ = resp.Body.Close()
			return nil
		}
	}
}
