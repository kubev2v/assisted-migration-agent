package models

// Credentials holds vCenter connection credentials.
type Credentials struct {
	URL      string
	Username string
	Password string
}

type VCenterCredentials struct {
	URL      string
	Username string
	Password string
}
