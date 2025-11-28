package models

import (
	api "github.com/kubev2v/migration-planner/api/v1alpha1"
)

type InfrastructureData struct {
	Datastores            []api.Datastore
	Networks              []api.Network
	HostPowerStates       map[string]int
	Hosts                 *[]api.Host
	HostsPerCluster       []int
	ClustersPerDatacenter []int
	TotalHosts            int
	TotalClusters         int
	TotalDatacenters      int
	VmsPerCluster         []int
}
