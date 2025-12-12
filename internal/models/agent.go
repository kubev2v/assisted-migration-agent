package models

import "fmt"

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

func ParseConsoleStatusType(s string) (ConsoleStatusType, error) {
	switch s {
	case "connected":
		return ConsoleStatusConnected, nil
	case "disconnected":
		return ConsoleStatusDisconnected, nil
	default:
		return "", fmt.Errorf("invalid console status type: %s", s)
	}
}

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

type AgentStatus struct {
	Console   ConsoleStatus
	Collector CollectorStatusType
}
