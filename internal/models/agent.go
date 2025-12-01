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

type CollectorStatus string

const (
	CollectorStatusWaitingForCredentials CollectorStatus = "waiting-for-credentials"
	CollectorStatusConnecting            CollectorStatus = "connecting"
	CollectorStatusConnected             CollectorStatus = "connected"
	CollectorStatusCollecting            CollectorStatus = "collecting"
	CollectorStatusCollected             CollectorStatus = "collected"
	CollectorStatusError                 CollectorStatus = "error"
)

type AgentStatus struct {
	Console   ConsoleStatus
	Collector CollectorStatus
}
