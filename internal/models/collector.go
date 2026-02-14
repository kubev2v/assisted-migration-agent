package models

import (
	"context"
)

// CollectorStateType represents the current state of the collector.
type CollectorStateType string

const (
	// CollectorStateReady - credentials saved, waiting for collection request
	CollectorStateReady CollectorStateType = "ready"
	// CollectorStateConnecting - verifying credentials with vCenter
	CollectorStateConnecting CollectorStateType = "connecting"
	// CollectorStateConnected - credentials verified
	CollectorStateConnected CollectorStateType = "connected"
	// CollectorStateCollecting - async collection in progress
	CollectorStateCollecting CollectorStateType = "collecting"
	// CollectorStateParsing - parsing collected data into duckdb
	CollectorStateParsing CollectorStateType = "parsing"
	// CollectorStateCollected - collection complete (auto-transitions to ready)
	CollectorStateCollected CollectorStateType = "collected"
	// CollectorStateError - error during connecting or collecting
	CollectorStateError CollectorStateType = "error"

	// V1 agent status
	CollectorLegacyStateWaitingForCredentials CollectorStateType = "waiting-for-credentials"
	CollectorLegacyStateCollecting            CollectorStateType = "gathering-initial-inventory"
	CollectorLegacyStateError                 CollectorStateType = "error"
	CollectorLegacyStateCollected             CollectorStateType = "up-to-date"
)

func (c CollectorStateType) ToV1() CollectorStateType {
	switch c {
	case CollectorStateReady:
		return CollectorLegacyStateWaitingForCredentials
	case CollectorStateConnecting, CollectorStateCollecting, CollectorStateParsing:
		return CollectorLegacyStateCollecting
	case CollectorStateCollected:
		return CollectorLegacyStateCollected
	case CollectorLegacyStateError:
		return CollectorLegacyStateError
	default:
		return "unknown state"
	}
}

// CollectorStatus holds the current collector state and metadata.
type CollectorStatus struct {
	State CollectorStateType
	Error error
}

type WorkBuilder interface {
	WithCredentials(creds *Credentials) WorkBuilder
	Build() []WorkUnit
}

// WorkUnit represents a unit of work in the collector workflow.
type WorkUnit struct {
	Status func() CollectorStatus
	Work   func() func(ctx context.Context) (any, error)
}
