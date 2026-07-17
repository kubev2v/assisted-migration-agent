// internal/services/alias.go
package services

import (
	v1 "github.com/kubev2v/assisted-migration-agent/internal/services/v1"
	v2 "github.com/kubev2v/assisted-migration-agent/internal/services/v2"
)

// Types — true Go type aliases; methods, embedding, and type assertions all work.
type ApplicationService = v1.ApplicationService
type BenchmarkResult = v1.BenchmarkResult
type BenchmarkStrategy = v1.BenchmarkStrategy
type Collector = v1.Collector
type CollectorService = v1.CollectorService
type Console = v1.Console
type CredentialsService = v1.CredentialsService
type EventService = v1.EventService
type ExportService = v1.ExportService
type ForecasterService = v1.ForecasterService
type GroupGetParams = v1.GroupGetParams
type GroupListParams = v1.GroupListParams
type GroupService = v1.GroupService
type InspectorService = v1.InspectorService
type InventoryBuilder = v1.InventoryBuilder
type InventoryService = v1.InventoryService
type RightsizingService = v1.RightsizingService
type ServiceManager = v1.ServiceManager
type ServiceManagerOption = v1.ServiceManagerOption
type SortField = v1.SortField
type VddkService = v1.VddkService
type VMListParams = v1.VMListParams
type VMService = v1.VMService

// Constructors and option funcs — func vars, called identically to the originals.
var (
	NewApplicationService               = v1.NewApplicationService
	NewCollectorService                 = v1.NewCollectorService
	NewConsoleService                   = v1.NewConsoleService
	NewCredentialsService               = v1.NewCredentialsService
	NewEventService                     = v1.NewEventService
	NewExportService                    = v1.NewExportService
	NewForecasterService                = v1.NewForecasterService
	NewGroupService                     = v1.NewGroupService
	NewGroupServiceWithInventoryBuilder = v1.NewGroupServiceWithInventoryBuilder
	NewInspectorService                 = v1.NewInspectorService
	NewInventoryService                 = v1.NewInventoryService
	NewRightsizingService               = v1.NewRightsizingService
	NewServiceManager                   = v1.NewServiceManager
	NewVddkService                      = v1.NewVddkService
	NewVMService                        = v1.NewVMService
	WithConfig                          = v1.WithConfig
	WithConsoleClient                   = v1.WithConsoleClient
	WithKeyManager                      = v1.WithKeyManager
	WithStore                           = v1.WithStore
)

// ── V2 type aliases ─────────────────────────────────────────────────────
type V2ApplicationService = v2.ApplicationService
type V2CollectorService = v2.CollectorService
type V2Console = v2.Console
type V2CredentialsService = v2.CredentialsService
type V2EventService = v2.EventService
type V2GroupService = v2.GroupService
type V2GroupGetParams = v2.GroupGetParams
type V2GroupListParams = v2.GroupListParams
type V2InventoryService = v2.InventoryService
type V2RightsizingService = v2.RightsizingService
type V2ServiceManager = v2.ServiceManager
type V2ServiceManagerOption = v2.ServiceManagerOption
type V2SortField = v2.SortField
type V2VMListParams = v2.VMListParams
type V2VMService = v2.VMService

// ── V2 constructors and option funcs ────────────────────────────────────
var (
	V2NewCollectorService   = v2.NewCollectorService
	V2NewConsoleService     = v2.NewConsoleService
	V2NewCredentialsService = v2.NewCredentialsService
	V2NewEventService       = v2.NewEventService
	V2NewInventoryService   = v2.NewInventoryService
	V2NewRightsizingService = v2.NewRightsizingService
	V2NewServiceManager     = v2.NewServiceManager
	V2NewVMService          = v2.NewVMService
	V2WithConfig            = v2.WithConfig
	V2WithConsoleClient     = v2.WithConsoleClient
	V2WithKeyManager        = v2.WithKeyManager
	V2WithPool              = v2.WithPool
	V2WithOpaValidator      = v2.WithOpaValidatior
)
