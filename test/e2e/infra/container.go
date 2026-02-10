package infra

import "fmt"

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
	runner       *PodmanRunner
	backendImage string
	agentImage   string
	isoPath      string
	oidc         *OIDCServer
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

func (c *ContainerInfraManager) StartVcsim() error {
	_, err := c.runner.StartContainer(
		NewContainerConfig(vcsimContainerName, vcsimImage).
			WithPort(vcsimPort, vcsimPort).
			WithCmd(
				"-l", ":8989",
				"-username", VcsimUsername,
				"-password", VcsimPassword,
				"-dc", "1",
				"-cluster", "1",
				"-ds", "1",
				"-host", "1",
				"-vm", "3",
			),
	)
	return err
}

func (c *ContainerInfraManager) StopVcsim() error {
	if err := c.runner.StopContainer(vcsimContainerName); err != nil {
		return err
	}
	return c.runner.RemoveContainer(vcsimContainerName)
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
		WithEnvVar("AGENT_MODE", cfg.Mode).
		WithEnvVar("AGENT_AGENT_ID", cfg.AgentID).
		WithEnvVar("AGENT_SOURCE_ID", cfg.SourceID).
		WithEnvVar("AGENT_DATA_FOLDER", "/var/lib/agent").
		WithEnvVar("AGENT_CONSOLE_URL", cfg.ConsoleURL).
		WithEnvVar("AGENT_CONSOLE_UPDATE_INTERVAL", updateInterval)

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
