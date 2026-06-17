package models

// OperationPermission describes whether stored credentials have sufficient
// vSphere privileges for a specific operation.
type OperationPermission struct {
	Allowed           bool     `json:"allowed"`
	MissingPrivileges []string `json:"missingPrivileges,omitempty"`
}

// PermissionStatus holds the permission check results for each operation type.
type PermissionStatus struct {
	Collector  OperationPermission `json:"collector"`
	Inspector  OperationPermission `json:"inspector"`
	Forecaster OperationPermission `json:"forecaster"`
}
