package models

type OperationCapability struct {
	Enabled           bool
	MissingPrivileges []string
}

type CapabilityStatus struct {
	Collector  OperationCapability
	Inspector  OperationCapability
	Forecaster OperationCapability
}
