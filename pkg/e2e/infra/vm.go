package infra

import "fmt"

// VMInfraManager implements InfraManager for VM-based deployments.
// Infrastructure (Postgres, backend, vcsim, agent) is managed externally
// (e.g., via Kind + deploy/e2e.mk). The OIDC server runs in-process
// since it is needed for token generation regardless of infra mode.
type VMInfraManager struct {
	oidc *OIDCServer
}

// NewVMInfraManager creates a new VMInfraManager.
func NewVMInfraManager() *VMInfraManager {
	return &VMInfraManager{}
}

func (v *VMInfraManager) StartOIDC(addr string) error {
	oidc, err := NewOIDCServer(addr)
	if err != nil {
		return err
	}
	v.oidc = oidc
	return nil
}

func (v *VMInfraManager) StopOIDC() error {
	if v.oidc != nil {
		return v.oidc.Stop()
	}
	return nil
}

func (v *VMInfraManager) GenerateToken(username, orgID, email string) (string, error) {
	if v.oidc == nil {
		return "", fmt.Errorf("OIDC server not started")
	}
	return v.oidc.GenerateToken(username, orgID, email)
}

func (v *VMInfraManager) StartPostgres() error { return nil }
func (v *VMInfraManager) StopPostgres() error  { return nil }
func (v *VMInfraManager) StartBackend() error  { return nil }
func (v *VMInfraManager) StopBackend() error   { return nil }
func (v *VMInfraManager) StartVcsim() error    { return nil }
func (v *VMInfraManager) StopVcsim() error     { return nil }
func (v *VMInfraManager) StopAgent() error     { return nil }
func (v *VMInfraManager) RestartAgent() error  { return nil }
func (v *VMInfraManager) RemoveAgent() error   { return nil }

func (v *VMInfraManager) StartAgent(_ AgentConfig) (string, error) {
	return "", nil
}
