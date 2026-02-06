package infra

// InfraManager abstracts infrastructure lifecycle for e2e tests.
// Container-based: starts/stops containers via Podman.
// VM-based: no-op, infrastructure is managed externally (kind + deploy/e2e.mk).
type InfraManager interface {
	StartOIDC(addr string) error
	StopOIDC() error
	GenerateToken(username, orgID, email string) (string, error)
	StartPostgres() error
	StopPostgres() error
	StartBackend() error
	StopBackend() error
	StartVcsim() error
	StopVcsim() error
	StartAgent(cfg AgentConfig) (string, error)
	StopAgent() error
	RestartAgent() error
	RemoveAgent() error
}

// AgentConfig holds configuration for starting an agent instance.
type AgentConfig struct {
	AgentID        string
	SourceID       string
	Mode           string // "connected" or "disconnected"
	ConsoleURL     string
	UpdateInterval string // e.g. "1s"
	ISOPath        string // Path to the bootable ISO on disk (VM mode: booted via libvirt)
}

const (
	VcsimUsername = "core"
	VcsimPassword = "123456"
)
