package v1

import (
	"github.com/kubev2v/assisted-migration-agent/internal/models"
)

func (a *AgentStatus) FromModel(m models.AgentStatus) {
	a.ConsoleConnection = AgentStatusConsoleConnection(m.Console.Current)
	a.Mode = AgentStatusMode(m.Console.Target)
}

func NewCollectorStatus(status models.CollectorStatus) CollectorStatus {
	var c CollectorStatus

	switch status.State {
	case models.CollectorStateReady:
		c.Status = CollectorStatusStatusReady
	case models.CollectorStateConnecting:
		c.Status = CollectorStatusStatusConnecting
	case models.CollectorStateConnected:
		c.Status = CollectorStatusStatusConnected
	case models.CollectorStateCollecting:
		c.Status = CollectorStatusStatusCollecting
	case models.CollectorStateCollected:
		c.Status = CollectorStatusStatusCollected
	case models.CollectorStateError:
		c.Status = CollectorStatusStatusError
	default:
		c.Status = CollectorStatusStatusReady
	}

	if status.Error != "" {
		c.Error = &status.Error
	}

	return c
}

func NewCollectorStatusWithError(status models.CollectorStatus, err error) CollectorStatus {
	c := NewCollectorStatus(status)
	if err != nil {
		errStr := err.Error()
		c.Error = &errStr
	}
	return c
}
