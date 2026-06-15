package models

import (
	"time"

	inventoryapi "github.com/kubev2v/migration-planner-common/api/inventory"
)

type InfrastructureData struct {
	Datastores            []inventoryapi.Datastore
	Networks              []inventoryapi.Network
	HostPowerStates       map[string]int
	Hosts                 *[]inventoryapi.Host
	HostsPerCluster       []int
	ClustersPerDatacenter []int
	TotalHosts            int
	TotalClusters         int
	TotalDatacenters      int
	VmsPerCluster         []int
}

// Inventory represents inventory data stored in the database.
type Inventory struct {
	Data      []byte
	CreatedAt time.Time
	UpdatedAt time.Time
}
