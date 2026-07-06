package models

type CollectionState string

const (
	CollectionStateRunning CollectionState = "running"
	CollectionStateFailed  CollectionState = "failed"
)

type Collection struct {
	Database string
	State    CollectionState
	Error    string
}
