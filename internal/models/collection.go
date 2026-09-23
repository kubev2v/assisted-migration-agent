package models

type CollectionState string

const (
	CollectionStateRunning       CollectionState = "running"
	CollectionStateFailed        CollectionState = "failed"
	CollectionStatePendingDelete CollectionState = "pending_delete"
)

type Collection struct {
	Database string
	State    CollectionState
	Error    string
}
