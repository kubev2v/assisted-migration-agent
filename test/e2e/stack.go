package main

const (
	dbContainerName      = "test-planner-db"
	AgentContainerName   = "test-planner-agent"
	backendContainerName = "test-planner"
	vcsimContainerName   = "test-vcsim"
	vcsimImage           = "docker.io/vmware/vcsim:latest"
	vcsimPort            = 8989
	VcsimUsername        = "core"
	VcsimPassword        = "123456"
	AgentVolumeName      = "test-agent-data"

	// Database configuration
	dbType     = "pgsql"
	dbHost     = "localhost"
	dbPort     = "5432"
	dbName     = "planner"
	dbUser     = "planner"
	dbPassword = "adminpass"
)

type Stack struct {
	Runner *PodmanRunner
	cfg    configuration
}

func NewStack(cfg configuration) (*Stack, error) {
	runner, err := NewPodmanRunner(cfg.PodmanSocket)
	if err != nil {
		return nil, err
	}
	return &Stack{Runner: runner, cfg: cfg}, nil
}

func (s *Stack) StartPostgres() error {
	_, err := s.Runner.StartContainer(
		NewContainerConfig(dbContainerName, "docker.io/library/postgres:17").
			WithPort(5432, 5432).
			WithEnvVar("POSTGRES_USER", dbUser).
			WithEnvVar("POSTGRES_PASSWORD", dbPassword).
			WithEnvVar("POSTGRES_DB", dbName),
	)
	return err
}

func (s *Stack) StartBackend() error {
	cfg := NewContainerConfig(backendContainerName, s.cfg.BackendImage).
		WithPort(7443, 7443).
		WithPort(3443, 3443).
		WithEnvVar("DB_TYPE", dbType).
		WithEnvVar("DB_HOST", dbHost).
		WithEnvVar("DB_PORT", dbPort).
		WithEnvVar("DB_NAME", dbName).
		WithEnvVar("DB_USER", dbUser).
		WithEnvVar("DB_PASS", dbPassword).
		WithEnvVar("MIGRATION_PLANNER_MIGRATIONS_FOLDER", "/app/migrations").
		WithEnvVar("MIGRATION_PLANNER_AUTH", "none").
		WithEnvVar("MIGRATION_PLANNER_AGENT_AUTH_ENABLED", "false")

	if s.cfg.IsoPath != "" {
		cfg = cfg.WithBindMount(s.cfg.IsoPath, "/iso").
			WithEnvVar("MIGRATION_PLANNER_ISO_PATH", "/iso/rhcos-live-iso.x86_64.iso")
	}

	_, err := s.Runner.StartContainer(cfg)
	return err
}

func (s *Stack) StopPostgres() error {
	if err := s.Runner.StopContainer(dbContainerName); err != nil {
		return err
	}
	return s.Runner.RemoveContainer(dbContainerName)
}

func (s *Stack) StopBackend() error {
	if err := s.Runner.StopContainer(backendContainerName); err != nil {
		return err
	}
	return s.Runner.RemoveContainer(backendContainerName)
}

func (s *Stack) StartVcsim() error {
	_, err := s.Runner.StartContainer(
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

func (s *Stack) StopVcsim() error {
	if err := s.Runner.StopContainer(vcsimContainerName); err != nil {
		return err
	}
	return s.Runner.RemoveContainer(vcsimContainerName)
}
