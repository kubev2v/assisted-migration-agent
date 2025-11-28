package models

type AgentMode string

const (
	AgentModeConnected    AgentMode = "connected"
	AgentModeDisconnected AgentMode = "disconnected"
)

type ConsoleStatusType string

const (
	ConsoleStatusDisconnected ConsoleStatusType = "disconnected"
	ConsoleStatusConnecting   ConsoleStatusType = "connecting"
	ConsoleStatusConnected    ConsoleStatusType = "connected"
	ConsoleStatusError        ConsoleStatusType = "error"
)

type ConsoleStatus struct {
	Current ConsoleStatusType
	Target  ConsoleStatusType
	Error   error
}

type CollectorStatusType string

const (
	CollectorStatusWaitingForCredentials CollectorStatusType = "waiting-for-credentials"
	CollectorStatusConnecting            CollectorStatusType = "connecting"
	CollectorStatusConnected             CollectorStatusType = "connected"
	CollectorStatusCollecting            CollectorStatusType = "collecting"
	CollectorStatusCollected             CollectorStatusType = "collected"
	CollectorStatusError                 CollectorStatusType = "error"
)

type CollectorStatus struct {
	Current CollectorStatusType
}

type AgentStatus struct {
	Console   ConsoleStatus
	Collector CollectorStatus
}
