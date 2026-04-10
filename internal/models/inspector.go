package models

// InspectorState represents the current state of the Inspector.
type InspectorState string

const (
	// InspectorStateReady - waiting for inspection request
	InspectorStateReady InspectorState = "ready"
	// InspectorStateRunning - running inspections on VMs
	InspectorStateRunning InspectorState = "running"
)

// InspectorStatus holds the current Inspector state.
type InspectorStatus struct {
	State InspectorState
}
