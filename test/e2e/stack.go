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
			WithEnvVar("POSTGRES_USER", "planner").
			WithEnvVar("POSTGRES_PASSWORD", "adminpass").
			WithEnvVar("POSTGRES_DB", "planner"),
	)
	return err
}

func (s *Stack) StartBackend() error {
	_, err := s.Runner.StartContainer(
		NewContainerConfig(backendContainerName, s.cfg.BackendImage).
			WithPort(7443, 7443).
			WithPort(3443, 3443).
			WithEnvVar("DATABASE_URL", s.cfg.Database),
	)
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
